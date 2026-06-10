package pikoci

import (
	"context"
	"fmt"

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

	if q.Notifier != nil {
		q.Notifier.Notify()
	}

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
