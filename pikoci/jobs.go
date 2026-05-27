package pikoci

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
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

	bb := build.Build{Status: build.Pending}

	// Pin the latest version of the first get-step resource so the version
	// is locked at trigger time rather than at execution time.
	getSteps := j.GetSteps()
	if len(getSteps) > 0 {
		g := getSteps[0]
		rCan := g.ResourceCanonical()
		vers, err := q.Resources.FilterVersions(ctx, tc, pc, rCan, nil, nil, 0)
		if err == nil && len(vers) > 0 {
			bb.ResourceCanonical = rCan
			bb.VersionID = vers[0].ID
		}
	}

	nb, err := q.CreateJobBuild(ctx, tc, pc, jn, bb)
	if err != nil {
		return fmt.Errorf("failed to create pending build for Job %q on Pipeline %q: %w", jn, pc, err)
	}

	m := queue.Body{
		TeamCanonical:     tc,
		PipelineCanonical: pc,
		JobName:           jn,
		BuildID:           nb.ID,
		ResourceCanonical: nb.ResourceCanonical,
		VersionID:         nb.VersionID,
	}

	mb, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal Message Body: %w", err)
	}

	err = q.JobTopic.Send(ctx, &pubsub.Message{
		Body: mb,
	})
	if err != nil {
		return fmt.Errorf("failed to Trigger Job %q on Pipeline %q: %w", jn, pc, err)
	}

	return nil
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
