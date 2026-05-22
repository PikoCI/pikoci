package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"gocloud.dev/pubsub"
)

// Scheduler polls the database for resources due for a check and sends messages to the topic.
type Scheduler struct {
	resources resource.Repository
	pipelines pipeline.Repository
	builds    build.Repository
	topic     queue.Topic
	logger    *slog.Logger
	interval  time.Duration
}

// New creates a new Scheduler.
func New(resources resource.Repository, pipelines pipeline.Repository, builds build.Repository, topic queue.Topic, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		resources: resources,
		pipelines: pipelines,
		builds:    builds,
		topic:     topic,
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

	for _, rwp := range due {
		s.logger.Info("Checking resource ...", "Pipeline", rwp.PipelineCanonical, "Resource", rwp.Canonical)

		m := queue.Body{
			TeamCanonical:     rwp.TeamCanonical,
			PipelineCanonical: rwp.PipelineCanonical,
			ResourceCanonical: rwp.Canonical,
		}
		mb, err := json.Marshal(m)
		if err != nil {
			s.logger.Error("failed to marshal Message Body", "error", err)
			continue
		}
		err = s.topic.Send(ctx, &pubsub.Message{
			Body: mb,
		})
		if err != nil {
			s.logger.Error("failed to send Topic", "error", err)
			continue
		}

		now := time.Now()
		rwp.Resource.LastCheck = now

		spec := rwp.CheckInterval
		if spec == "" {
			spec = "@every 1m"
		}
		nextCheck, err := ComputeNextCheck(spec, now)
		if err != nil {
			s.logger.Error("failed to compute next check", "error", err)
			continue
		}
		rwp.Resource.NextCheck = nextCheck

		err = s.resources.Update(ctx, rwp.TeamCanonical, rwp.PipelineCanonical, rwp.Canonical, rwp.Resource)
		if err != nil {
			s.logger.Error("failed to update resource", "error", err)
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

		versionID, ready, err := s.builds.FindReadyDownstreamVersion(
			ctx, pwt.Team.Canonical, pwt.Canonical,
			g.Passed, j.Name, g.Name, len(g.Passed),
		)
		if err != nil {
			s.logger.Error("failed to find ready downstream version",
				"pipeline", pwt.Canonical, "job", j.Name, "error", err)
			return
		}
		if !ready {
			return
		}
		candidates = append(candidates, candidate{g.Name, g.Passed, versionID})
	}

	if len(candidates) == 0 {
		return
	}

	// Use the version from the first candidate for the queue message.
	versionID := candidates[0].versionID

	s.logger.Info("[debug-297] Triggering downstream job",
		"pipeline", pwt.Canonical, "job", j.Name, "version_id", versionID,
		"candidates", fmt.Sprintf("%+v", candidates))

	m := queue.Body{
		TeamCanonical:     pwt.Team.Canonical,
		PipelineCanonical: pwt.Canonical,
		JobName:           j.Name,
		VersionID:         versionID,
	}
	mb, err := json.Marshal(m)
	if err != nil {
		s.logger.Error("failed to marshal downstream trigger body", "error", err)
		return
	}
	if err := s.topic.Send(ctx, &pubsub.Message{Body: mb}); err != nil {
		s.logger.Error("failed to send downstream trigger message",
			"job", j.Name, "error", err)
	}
}
