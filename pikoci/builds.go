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

var ErrConcurrencyLimit = errors.New("concurrency limit reached")

func (q *PikoCI) CreateJobBuild(ctx context.Context, tc, pc, jn string, b build.Build) (*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		if err := checkConcurrencyLimit(ctx, uow, tc, pc, jn); err != nil {
			return err
		}

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

func (q *PikoCI) ListJobBuilds(ctx context.Context, tc, pc, jn string) ([]*build.Build, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	builds, err := q.Builds.Filter(ctx, tc, pc, jn)
	if err != nil {
		return nil, fmt.Errorf("failed to list Builds: %w", err)
	}

	slices.Reverse(builds)

	return builds, nil
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
	if b.Status != build.Started {
		return fmt.Errorf("build %s is not running (status: %s)", buildNumber, b.Status)
	}
	b.Status = build.Cancelled
	b.Duration = time.Since(b.StartedAt)
	return q.Builds.Update(ctx, tc, pc, jn, buildNumber, *b)
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
	if b.Status == build.Started {
		return fmt.Errorf("build %s is still running", buildNumber)
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

	m := queue.Body{
		TeamCanonical:     tc,
		PipelineCanonical: pc,
		JobName:           jn,
		RetryBuildNumber:  parentBN,
		RetryBuildID:      retryBuildID,
	}

	mb, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal Message Body: %w", err)
	}

	err = q.Topic.Send(ctx, &pubsub.Message{
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

	err := q.StartUoW(ctx, func(uow unitwork.UnitOfWork) error {
		if err := checkConcurrencyLimit(ctx, uow, tc, pc, jn); err != nil {
			return err
		}

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

func checkConcurrencyLimit(ctx context.Context, uow unitwork.UnitOfWork, tc, pc, jn string) error {
	j, err := uow.Jobs().Find(ctx, tc, pc, jn)
	if err != nil {
		return fmt.Errorf("failed to find job: %w", err)
	}
	if j.Concurrency > 0 {
		running, err := uow.Builds().CountRunning(ctx, tc, pc, jn)
		if err != nil {
			return fmt.Errorf("failed to count running builds: %w", err)
		}
		if running >= j.Concurrency {
			return ErrConcurrencyLimit
		}
	}
	return nil
}
