// Package scheduler implements the background polling loop that triggers
// downstream job builds when input constraints are satisfied, and notifies
// workers when resources become due for checking.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
)

// Scheduler polls the database for due resources and ready downstream jobs.
type Scheduler struct {
	resources resource.Repository
	pipelines pipeline.Repository
	builds    build.Repository
	notifier  *notifier.WorkNotifier
	logger    *slog.Logger
	interval  time.Duration
}

// New creates a new Scheduler.
func New(resources resource.Repository, pipelines pipeline.Repository, builds build.Repository, wn *notifier.WorkNotifier, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		resources: resources,
		pipelines: pipelines,
		builds:    builds,
		notifier:  wn,
		logger:    logger,
		interval:  10 * time.Second,
	}
}

// Start launches the polling goroutine that ticks every interval.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}

func (s *Scheduler) tick(ctx context.Context) {
	s.tickResources(ctx)
	s.tickJobs(ctx)
}

func (s *Scheduler) tickResources(ctx context.Context) {
	due, err := s.resources.FilterDueResources(ctx)
	if err != nil {
		s.logger.Error("failed to filter due resources", "error", err)
		return
	}

	if len(due) > 0 {
		s.logger.Info("Found due resources", "count", len(due))
		if s.notifier != nil {
			s.notifier.Notify()
		}
	}
}

func (s *Scheduler) tickJobs(ctx context.Context) {
	pps, err := s.pipelines.FilterAll(ctx)
	if err != nil {
		s.logger.Error("failed to filter all pipelines", "error", err)
		return
	}

	for _, pwt := range pps {
		for _, j := range pwt.Jobs {
			if j.Paused {
				continue
			}
			s.evaluateJob(ctx, pwt, &j)
		}
	}
}

// evaluateJob checks whether a job with passed constraints is ready to run.
// It checks ALL get steps with passed+trigger; if any is not ready, the job
// is skipped. Once triggered, it breaks — a job is only queued once per tick.
func (s *Scheduler) evaluateJob(ctx context.Context, pwt *pipeline.WithTeam, j *job.Job) {
	// Collect all get steps that have passed+trigger constraints.
	// ALL must be ready for the job to trigger.
	type candidate struct {
		stepName  string
		passed    []string
		versionID uint32
	}
	var candidates []candidate

	for _, ps := range j.Plan {
		if ps.Type != job.StepTypeGet || ps.Get == nil {
			continue
		}
		g := ps.Get
		if len(g.Passed) == 0 || !g.Trigger {
			continue
		}

		// Expand for_each group names to instance names
		expandedPassed := resolvePassedJobNames(g.Passed, pwt.Jobs)

		versionID, ready, err := s.builds.FindReadyDownstreamVersion(
			ctx, pwt.Team.Canonical, pwt.Canonical,
			expandedPassed, j.Name, g.Name, len(expandedPassed),
		)
		if err != nil {
			s.logger.Error("failed to find ready downstream version",
				"pipeline", pwt.Canonical, "job", j.Name, "error", err)
			return
		}
		if !ready {
			return
		}

		// Check if the resource is pinned to a different version
		rCan := g.ResourceCanonical()
		res, resErr := s.resources.Find(ctx, pwt.Team.Canonical, pwt.Canonical, rCan)
		if resErr != nil {
			s.logger.Error("failed to find resource for pin check, skipping job",
				"pipeline", pwt.Canonical, "job", j.Name, "resource", rCan, "error", resErr)
			return
		}
		if res.PinnedVersionID != nil && versionID != *res.PinnedVersionID {
			return // resource is pinned to a different version, skip
		}

		candidates = append(candidates, candidate{g.Name, g.Passed, versionID})
	}

	if len(candidates) == 0 {
		return
	}

	// If there's already a pending build for this job, skip — don't create
	// another one. This prevents the scheduler from re-triggering the same
	// unconsumed version every tick.
	pending, err := s.builds.FindOldestPending(ctx, pwt.Team.Canonical, pwt.Canonical, j.Name)
	if err != nil {
		s.logger.Error("failed to check for pending builds",
			"pipeline", pwt.Canonical, "job", j.Name, "error", err)
		return
	}
	if pending != nil {
		return
	}

	// Use the version from the first candidate.
	versionID := candidates[0].versionID

	s.logger.Info("Triggering downstream job",
		"pipeline", pwt.Canonical, "job", j.Name, "version_id", versionID,
		"candidates", fmt.Sprintf("%+v", candidates))

	// Create a pending build
	bb := build.Build{
		Status:    build.Pending,
		VersionID: versionID,
	}
	id, buildNumber, err := s.builds.Create(ctx, pwt.Team.Canonical, pwt.Canonical, j.Name, bb)
	if err != nil {
		s.logger.Error("failed to create pending build for downstream job",
			"pipeline", pwt.Canonical, "job", j.Name, "error", err)
		return
	}

	s.logger.Info("created pending build for downstream job",
		"pipeline", pwt.Canonical, "job", j.Name, "build_id", id, "build_number", buildNumber)

	if s.notifier != nil {
		s.notifier.Notify()
	}
}

// resolvePassedJobNames expands for_each group names in a passed list to all
// instance names. If a name matches a for_each group, it is replaced by all
// instance names in that group. Non-group names are kept as-is.
func resolvePassedJobNames(passed []string, jobs []job.Job) []string {
	groupInstances := make(map[string][]string)
	for _, j := range jobs {
		if j.ForEachGroup != "" {
			groupInstances[j.ForEachGroup] = append(groupInstances[j.ForEachGroup], j.Name)
		}
	}

	var expanded []string
	for _, name := range passed {
		if instances, ok := groupInstances[name]; ok {
			expanded = append(expanded, instances...)
		} else {
			expanded = append(expanded, name)
		}
	}
	return expanded
}
