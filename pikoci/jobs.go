package pikoci

import (
	"context"
	"fmt"
	"strings"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// TriggerPipelineJob creates a pending build for the specified job, pins the
// latest version of its first get-step resource, and enqueues the build for
// execution via the job topic.
func (q *PikoCI) TriggerPipelineJob(ctx context.Context, tc, pc, jn string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	j, err := q.Jobs.Find(ctx, tc, pc, jn)
	if err != nil {
		return fmt.Errorf("failed to Find Job %q on Pipeline %q: %w", jn, pc, err)
	}

	if j.Paused {
		return fmt.Errorf("job %q is paused", jn)
	}

	bb := build.Build{Status: build.Pending}

	// Use the pinned version if the resource is pinned, otherwise use the
	// latest version of the first get-step resource.
	getSteps := j.GetSteps()
	if len(getSteps) > 0 {
		g := getSteps[0]
		rCan := g.ResourceCanonical()
		r, rerr := q.Resources.Find(ctx, tc, pc, rCan)
		if rerr == nil && r.PinnedVersionID != nil {
			bb.ResourceCanonical = rCan
			bb.VersionID = *r.PinnedVersionID
		} else {
			vers, err := q.Resources.FilterVersions(ctx, tc, pc, rCan, nil, nil, 0)
			if err == nil && len(vers) > 0 {
				bb.ResourceCanonical = rCan
				bb.VersionID = vers[0].ID
			}
		}
	}

	_, err = q.CreateJobBuild(ctx, tc, pc, jn, bb)
	if err != nil {
		return fmt.Errorf("failed to create pending build for Job %q on Pipeline %q: %w", jn, pc, err)
	}

	q.Notifier.Notify()

	return nil
}

// PauseJob pauses a specific job within a pipeline.
func (q *PikoCI) PauseJob(ctx context.Context, tc, pc, jn string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}
	return q.Jobs.SetPaused(ctx, tc, pc, jn, true)
}

// UnpauseJob unpauses a specific job within a pipeline.
func (q *PikoCI) UnpauseJob(ctx context.Context, tc, pc, jn string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}
	return q.Jobs.SetPaused(ctx, tc, pc, jn, false)
}

// ListPipelineJobs returns all jobs for the given pipeline enriched with
// their latest build status information.
func (q *PikoCI) ListPipelineJobs(ctx context.Context, tc, pn string) ([]job.WithStatus, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pn)
	}

	pp, err := q.Pipelines.Find(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pipeline %q: %w", pn, err)
	}

	return q.enrichJobsWithStatus(ctx, tc, pn, pp.Jobs)
}

// ListPublicPipelineJobs returns all jobs for a public pipeline enriched
// with their latest build status.
func (q *PikoCI) ListPublicPipelineJobs(ctx context.Context, tc, pn string) ([]job.WithStatus, error) {
	pp, err := q.Pipelines.FindPublic(ctx, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("pipeline not found or not public: %w", err)
	}

	return q.enrichJobsWithStatus(ctx, tc, pn, pp.Jobs)
}

// enrichJobsWithStatus enriches a slice of jobs with build status information.
func (q *PikoCI) enrichJobsWithStatus(ctx context.Context, tc, pn string, jobs []job.Job) ([]job.WithStatus, error) {
	result := make([]job.WithStatus, 0, len(jobs))
	for _, j := range jobs {
		ws := job.WithStatus{Job: j}

		builds, err := q.Builds.Filter(ctx, tc, pn, j.Name, nil, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to filter builds for job %q: %w", j.Name, err)
		}

		// Find latest completed build (including retries) and any running build.
		// Same logic as generateImage in pipelines.go.
		var cb *build.Build
		var rb *build.Build
		for _, b := range builds {
			if b.Status == build.Started && (rb == nil || rb.Status == build.Pending) {
				rb = b
			} else if b.Status == build.Pending && rb == nil {
				rb = b
			}
		}
		seen := map[string]bool{}
		for cb == nil {
			var mainBN string
			for _, b := range builds {
				if !strings.Contains(b.BuildNumber, ".") && !seen[b.BuildNumber] {
					mainBN = b.BuildNumber
					break
				}
			}
			if mainBN == "" {
				break
			}
			seen[mainBN] = true
			for _, b := range builds {
				if b.BuildNumber == mainBN || strings.HasPrefix(b.BuildNumber, mainBN+".") {
					if b.Status != build.Started && b.Status != build.Pending {
						cb = b
						break
					}
				}
			}
		}

		if cb != nil {
			ws.LatestStatus = cb.Status.String()
			ws.LatestBuildNumber = cb.BuildNumber
			ws.LatestBuildDuration = int64(cb.Duration)
			if !cb.StartedAt.IsZero() {
				t := cb.StartedAt
				ws.StartedAt = &t
			}
		}

		if rb != nil {
			ws.HasRunning = true
			if cb == nil {
				ws.LatestBuildNumber = rb.BuildNumber
				if !rb.StartedAt.IsZero() {
					t := rb.StartedAt
					ws.StartedAt = &t
				}
			}
		}

		result = append(result, ws)
	}
	return result, nil
}

// GetPipelineJob retrieves a job by its name within a pipeline.
func (q *PikoCI) GetPipelineJob(ctx context.Context, tc, pc, jn string) (*job.Job, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	j, err := q.Jobs.Find(ctx, tc, pc, jn)
	if err != nil {
		return nil, fmt.Errorf("failed to Find Job %q on Pipeline %q: %w", jn, pc, err)
	}

	return j, nil
}
