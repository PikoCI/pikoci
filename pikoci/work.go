package pikoci

import (
	"context"
	"time"

	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/scheduler"
)

// NextWork finds the next available work item. It first scans all pipelines for
// pending builds that can be started, then checks for due resource checks. Returns
// nil if no work is available.
func (q *PikoCI) NextWork(ctx context.Context) (*queue.WorkItem, error) {
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

			return &queue.WorkItem{
				Type: "job",
				Body: queue.Body{
					TeamCanonical:     pwt.Team.Canonical,
					PipelineCanonical: pwt.Canonical,
					JobName:           j.Name,
					BuildID:           pending.ID,
					VersionID:         pending.VersionID,
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

		return &queue.WorkItem{
			Type: "check",
			Body: queue.Body{
				TeamCanonical:     rwp.TeamCanonical,
				PipelineCanonical: rwp.PipelineCanonical,
				ResourceCanonical: rwp.Canonical,
			},
		}, nil
	}

	return nil, nil
}

// PollNextWork blocks until work is available or a 30-second timeout expires.
// It first checks for immediately available work, then waits for a notification
// from the WorkNotifier before checking again.
func (q *PikoCI) PollNextWork(ctx context.Context) (*queue.WorkItem, error) {
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
