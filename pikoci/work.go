package pikoci

import (
	"context"
	"errors"
	"time"

	"github.com/pikoci/pikoci/pikoci/scheduler"
	"github.com/pikoci/pikoci/pikoci/workitem"
)

// NextWork finds the next available work item by scanning all pipelines for
// pending builds, then checking for due resource checks. It returns nil if
// no work is available. For job builds, it calls StartPendingBuild to claim
// the build atomically, respecting concurrency and serial group limits.
func (q *PikoCI) NextWork(ctx context.Context) (*workitem.Item, error) {
	// Phase 1: Look for pending job builds.
	pps, err := q.Pipelines.FilterAll(ctx)
	if err != nil {
		return nil, err
	}

	for _, pwt := range pps {
		for _, j := range pwt.Jobs {
			if j.Paused {
				continue
			}

			pending, err := q.Builds.FindOldestPending(ctx, pwt.Team.Canonical, pwt.Canonical, j.Name)
			if err != nil {
				q.logger.Error("NextWork: failed to find oldest pending build",
					"pipeline", pwt.Canonical, "job", j.Name, "error", err)
				continue
			}
			if pending == nil {
				continue
			}

			// Attempt to start the build. This checks concurrency limits
			// and serial groups atomically. If the build cannot start,
			// skip to the next job.
			started, err := q.StartPendingBuild(ctx, pwt.Team.Canonical, pwt.Canonical, j.Name, pending.ID)
			if err != nil {
				if errors.Is(err, ErrBuildNotPending) ||
					errors.Is(err, ErrConcurrencyLimit) ||
					errors.Is(err, ErrJobPaused) ||
					errors.Is(err, ErrSerialGroupLimit) {
					continue
				}
				q.logger.Error("NextWork: failed to start pending build",
					"pipeline", pwt.Canonical, "job", j.Name, "build_id", pending.ID, "error", err)
				continue
			}

			return &workitem.Item{
				Type: "job",
				Body: workitem.Body{
					TeamCanonical:     pwt.Team.Canonical,
					PipelineCanonical: pwt.Canonical,
					JobName:           j.Name,
					BuildID:           started.ID,
					BuildNumber:       started.BuildNumber,
					VersionID:         started.VersionID,
				},
			}, nil
		}
	}

	// Phase 2: Look for due resource checks.
	due, err := q.Resources.FilterDueResources(ctx)
	if err != nil {
		return nil, err
	}

	for _, rwp := range due {
		now := time.Now()
		rwp.Resource.LastCheck = now

		spec := rwp.CheckInterval
		if spec == "" {
			spec = "@every 1m"
		}
		nextCheck, err := scheduler.ComputeNextCheck(spec, now)
		if err != nil {
			q.logger.Error("NextWork: failed to compute next check",
				"pipeline", rwp.PipelineCanonical, "resource", rwp.Canonical, "error", err)
			continue
		}
		rwp.Resource.NextCheck = nextCheck

		err = q.Resources.Update(ctx, rwp.TeamCanonical, rwp.PipelineCanonical, rwp.Canonical, rwp.Resource)
		if err != nil {
			q.logger.Error("NextWork: failed to update resource",
				"pipeline", rwp.PipelineCanonical, "resource", rwp.Canonical, "error", err)
			continue
		}

		return &workitem.Item{
			Type: "check",
			Body: workitem.Body{
				TeamCanonical:     rwp.TeamCanonical,
				PipelineCanonical: rwp.PipelineCanonical,
				ResourceCanonical: rwp.Canonical,
			},
		}, nil
	}

	return nil, nil
}

// PollNextWork blocks until work is available or a 30-second timeout expires.
// It checks for immediately available work first, then waits for a notification
// from the WorkNotifier before checking again.
func (q *PikoCI) PollNextWork(ctx context.Context) (*workitem.Item, error) {
	w, err := q.NextWork(ctx)
	if err != nil || w != nil {
		return w, err
	}

	ch, cleanup := q.Notifier.Wait()
	defer cleanup()

	select {
	case <-ch:
		return q.NextWork(ctx)
	case <-time.After(30 * time.Second):
		return q.NextWork(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
