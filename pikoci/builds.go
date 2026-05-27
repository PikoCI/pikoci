package pikoci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/unitwork"
	"github.com/xescugc/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
)

var (
	ErrConcurrencyLimit = errors.New("concurrency limit reached")
	ErrBuildNotPending  = build.ErrNotPending
)

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

func (q *PikoCI) InsertBuildGetVersion(ctx context.Context, tc, pc, jn string, buildID uint32, stepName string, versionID uint32) error {
	return q.Builds.InsertGetVersion(ctx, tc, pc, jn, buildID, stepName, versionID)
}

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
