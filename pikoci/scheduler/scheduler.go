// Package scheduler implements the background polling loop that triggers
// downstream job builds when input constraints are satisfied, and notifies
// workers when resources become due for checking.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
)

// DownstreamEvaluator is the narrow interface the scheduler uses to evaluate
// whether downstream jobs are ready to run.
type DownstreamEvaluator interface {
	EvaluateDownstreamJobs(ctx context.Context, tc, pn, completedJobName string) error
}

// Scheduler polls the database for due resources and ready downstream jobs.
type Scheduler struct {
	resources resource.Repository
	pipelines pipeline.Repository
	evaluator DownstreamEvaluator
	notifier  *notifier.WorkNotifier
	logger    *slog.Logger
	interval  time.Duration
}

// New creates a new Scheduler.
func New(resources resource.Repository, pipelines pipeline.Repository, wn *notifier.WorkNotifier, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		resources: resources,
		pipelines: pipelines,
		notifier:  wn,
		logger:    logger,
		interval:  10 * time.Second,
	}
}

// SetEvaluator sets the DownstreamEvaluator used by tickJobs.
func (s *Scheduler) SetEvaluator(e DownstreamEvaluator) {
	s.evaluator = e
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
		s.notifier.Notify()
	}
}

func (s *Scheduler) tickJobs(ctx context.Context) {
	if s.evaluator == nil {
		return
	}
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
			if err := s.evaluator.EvaluateDownstreamJobs(ctx, pwt.Team.Canonical, pwt.Canonical, j.Name); err != nil {
				s.logger.Error("failed to evaluate downstream jobs",
					"pipeline", pwt.Canonical, "job", j.Name, "error", err)
			}
		}
	}
}
