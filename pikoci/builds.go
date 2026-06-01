package pikoci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
)

var (
	// ErrConcurrencyLimit is returned when a build cannot be started because the
	// job's concurrency limit has been reached.
	ErrConcurrencyLimit = errors.New("concurrency limit reached")
	// ErrBuildNotPending is returned when attempting to start a build that is not
	// in the pending state.
	ErrBuildNotPending = build.ErrNotPending
	// ErrJobPaused is returned when attempting to start a build for a paused job.
	ErrJobPaused = errors.New("job is paused")
)

// CreateJobBuild creates a new pending build for the specified job within a unit
// of work. The build status is always set to pending regardless of the input.
func (q *PikoCI) CreateJobBuild(ctx context.Context, tc, pc, jn string, b build.Build) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	b.Status = build.Pending

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		id, buildNumber, err := uow.Builds().Create(ctx, tc, pc, jn, b)
		if err != nil {
			return fmt.Errorf("failed to Create Build: %w", err)
		}

		b.ID = id
		b.BuildNumber = buildNumber
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &b, nil
}

// ListJobBuilds returns paginated builds for a job, supporting cursor-based
// pagination with before and after parameters. Results are returned in
// newest-first order. The boolean return value indicates whether more results
// exist beyond the requested page.
func (q *PikoCI) ListJobBuilds(ctx context.Context, tc, pc, jn string, before *uint32, after *uint32, limit uint32) ([]*build.Build, bool, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, false, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, false, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, false, fmt.Errorf("invalid Job Name format %q", jn)
	}

	fetchLimit := limit
	if limit > 0 {
		fetchLimit = limit + 1
	}

	builds, err := q.Builds.Filter(ctx, tc, pc, jn, before, after, fetchLimit)
	if err != nil {
		return nil, false, fmt.Errorf("failed to list Builds: %w", err)
	}

	hasMore := false
	if limit > 0 && uint32(len(builds)) > limit {
		hasMore = true
		builds = builds[:limit]
	}

	// For "after" queries the DB returns ASC order; reverse to newest-first
	if after != nil {
		slices.Reverse(builds)
	}

	return builds, hasMore, nil
}

// GetJobBuild retrieves a single build by its build number.
func (q *PikoCI) GetJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to Find Build: %w", err)
	}
	return b, nil
}

// CancelJobBuild cancels a running or pending build and notifies the next
// pending build in the queue so it can potentially start.
func (q *PikoCI) CancelJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Find Build: %w", err)
	}
	wasRunning := b.Status == build.Started
	if b.Status != build.Started && b.Status != build.Pending {
		return fmt.Errorf("build %s is not running or pending (status: %s)", buildNumber, b.Status)
	}
	b.Status = build.Cancelled
	if wasRunning {
		b.Duration = time.Since(b.StartedAt)
	}
	if err := q.Builds.Update(ctx, tc, pc, jn, buildNumber, *b); err != nil {
		return err
	}

	// Notify the next pending build so it can start (whether we cancelled
	// a running or a pending build, a concurrency slot may have opened).
	q.notifyNextPendingBuild(ctx, tc, pc, jn)
	return nil
}

// UpdateJobBuild updates an existing build's state and metadata. It prevents
// workers from overwriting a cancelled build back to a non-terminal status and
// automatically computes the duration for completed builds.
func (q *PikoCI) UpdateJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string, b build.Build) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	// Prevent worker from overwriting a cancelled build back to a non-terminal status
	existing, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err == nil && existing.Status == build.Cancelled {
		b.Status = build.Cancelled
	}

	if b.Status != build.Started && b.Duration == 0 {
		b.Duration = time.Since(b.StartedAt)
	}

	if err = q.Builds.Update(ctx, tc, pc, jn, buildNumber, b); err != nil {
		return fmt.Errorf("failed to Update Build: %w", err)
	}

	return nil
}

// InsertBuildGetVersion records the resource version fetched by a get step
// during a build execution.
func (q *PikoCI) InsertBuildGetVersion(ctx context.Context, tc, pc, jn string, buildID uint32, stepName string, versionID uint32) error {
	return q.Builds.InsertGetVersion(ctx, tc, pc, jn, buildID, stepName, versionID)
}

// DeleteJobBuild removes a build by its build number.
func (q *PikoCI) DeleteJobBuild(ctx context.Context, tc, pc, jn string, buildNumber string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	err := q.Builds.Delete(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Delete Build: %w", err)
	}

	return nil
}

// RetryJobBuild creates a retry of a completed build and enqueues it for
// execution. The retry inherits the resource versions from the original parent
// build to ensure consistency.
func (q *PikoCI) RetryJobBuild(ctx context.Context, tc, pc, jn, buildNumber string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	b, err := q.Builds.Find(ctx, tc, pc, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to Find Build: %w", err)
	}
	if b.Status == build.Started || b.Status == build.Pending {
		return fmt.Errorf("build %s is still running or pending", buildNumber)
	}

	// Extract parent build number: if "3.1" -> "3", if "3" -> "3"
	parentBN := buildNumber
	if idx := strings.Index(buildNumber, "."); idx != -1 {
		parentBN = buildNumber[:idx]
	}

	// Always resolve versions from the original (parent) build so retries
	// of retries still get the correct versions even if the intermediate
	// retry failed before completing its get steps.
	retryBuildID := b.ID
	if parentBN != buildNumber {
		parentBuild, err := q.Builds.Find(ctx, tc, pc, jn, parentBN)
		if err != nil {
			return fmt.Errorf("failed to find parent build %q: %w", parentBN, err)
		}
		retryBuildID = parentBuild.ID
	}

	// Create a pending retry build first
	nb, err := q.CreateRetryJobBuild(ctx, tc, pc, jn, parentBN, build.Build{Status: build.Pending})
	if err != nil {
		return fmt.Errorf("failed to create pending retry build: %w", err)
	}

	m := queue.Body{
		TeamCanonical:     tc,
		PipelineCanonical: pc,
		JobName:           jn,
		BuildID:           nb.ID,
		RetryBuildNumber:  parentBN,
		RetryBuildID:      retryBuildID,
	}

	mb, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal Message Body: %w", err)
	}

	err = q.JobTopic.Send(ctx, &pubsub.Message{
		Body: mb,
	})
	if err != nil {
		return fmt.Errorf("failed to enqueue retry for Build %q: %w", buildNumber, err)
	}

	return nil
}

// CreateRetryJobBuild creates a retry build under the given parent build number
// within a unit of work. The retry build number is formatted as "parent.N"
// where N is the retry sequence number.
func (q *PikoCI) CreateRetryJobBuild(ctx context.Context, tc, pc, jn, parentBuildNumber string, b build.Build) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	b.Status = build.Pending

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		id, buildNumber, err := uow.Builds().CreateRetry(ctx, tc, pc, jn, parentBuildNumber, b)
		if err != nil {
			return fmt.Errorf("failed to Create Retry Build: %w", err)
		}

		b.ID = id
		b.BuildNumber = buildNumber
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &b, nil
}

// FindBuildGetVersions returns the resource version IDs fetched during get
// steps of the specified build, keyed by step name.
func (q *PikoCI) FindBuildGetVersions(ctx context.Context, tc, pc, jn string, buildID uint32) (map[string]uint32, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	return q.Builds.FindGetVersions(ctx, buildID)
}

// StartPendingBuild transitions a pending build to started status. It checks the
// job's concurrency limit and returns ErrConcurrencyLimit if the limit has been
// reached.
func (q *PikoCI) StartPendingBuild(ctx context.Context, tc, pn, jn string, buildID uint32) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		j, err := uow.Jobs().Find(ctx, tc, pn, jn)
		if err != nil {
			return fmt.Errorf("failed to find job: %w", err)
		}
		if j.Paused {
			return ErrJobPaused
		}
		if j.Concurrency > 0 {
			running, err := uow.Builds().CountRunning(ctx, tc, pn, jn)
			if err != nil {
				return fmt.Errorf("failed to count running builds: %w", err)
			}
			if running >= j.Concurrency {
				return ErrConcurrencyLimit
			}
		}

		return uow.Builds().StartPending(ctx, tc, pn, jn, buildID)
	})
	if err != nil {
		return nil, err
	}

	return q.Builds.FindByID(ctx, buildID)
}

// FindOldestPendingBuild returns the oldest pending build for the specified job,
// or nil if no pending builds exist.
func (q *PikoCI) FindOldestPendingBuild(ctx context.Context, tc, pn, jn string) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	return q.Builds.FindOldestPending(ctx, tc, pn, jn)
}

// notifyNextPendingBuild finds the oldest pending build for the job and sends a
// message to the job topic so it can be picked up for execution. This is called
// after a build is cancelled or completes to fill any freed concurrency slots.
func (q *PikoCI) notifyNextPendingBuild(ctx context.Context, tc, pc, jn string) {
	pending, err := q.Builds.FindOldestPending(ctx, tc, pc, jn)
	if err != nil || pending == nil {
		return
	}
	msg := queue.Body{
		TeamCanonical:     tc,
		PipelineCanonical: pc,
		JobName:           jn,
		BuildID:           pending.ID,
	}
	mb, err := json.Marshal(msg)
	if err != nil {
		return
	}
	q.JobTopic.Send(ctx, &pubsub.Message{Body: mb})
}

// ReEnqueuePendingBuilds scans all jobs for pending builds and re-publishes
// queue messages so workers can pick them up. This is called on server startup
// to recover builds that were stranded when the previous server instance stopped.
//
// This is safe to call even if some messages are still in the queue: the worker
// does not trust the BuildID in the message — it always calls FindOldestPending
// on the DB to determine which build to run (worker/service.go:238-266).
// Duplicate messages simply result in redundant DB lookups with no side effects.
func (q *PikoCI) ReEnqueuePendingBuilds(ctx context.Context) error {
	pps, err := q.Pipelines.FilterAll(ctx)
	if err != nil {
		return fmt.Errorf("filtering all pipelines: %w", err)
	}

	var count int
	for _, pwt := range pps {
		tc := pwt.Team.Canonical
		pc := pwt.Canonical
		for _, j := range pwt.Jobs {
			pending, err := q.Builds.FindOldestPending(ctx, tc, pc, j.Name)
			if err != nil {
				return fmt.Errorf("finding oldest pending build for %s/%s/%s: %w", tc, pc, j.Name, err)
			}
			if pending == nil {
				continue
			}
			msg := queue.Body{
				TeamCanonical:     tc,
				PipelineCanonical: pc,
				JobName:           j.Name,
				BuildID:           pending.ID,
			}
			mb, err := json.Marshal(msg)
			if err != nil {
				return fmt.Errorf("marshalling queue body: %w", err)
			}
			if err := q.JobTopic.Send(ctx, &pubsub.Message{Body: mb}); err != nil {
				return fmt.Errorf("sending queue message for %s/%s/%s: %w", tc, pc, j.Name, err)
			}
			count++
		}
	}

	if q.logger != nil && count > 0 {
		q.logger.Info("re-enqueued pending builds on startup", "count", count)
	}
	return nil
}
