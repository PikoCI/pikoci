package pikoci

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
)

func (q *PikoCI) TriggerPipelineJob(ctx context.Context, tc, pn, jn string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateCanonical(jn) {
		return fmt.Errorf("invalid Job Name format %q", jn)
	}

	j, err := q.Jobs.Find(ctx, tc, pn, jn)
	if err != nil {
		return fmt.Errorf("failed to Find Job %q on Pipeline %q: %w", jn, pn, err)
	}

	m := queue.Body{
		TeamCanonical: tc,
		PipelineName:  pn,
		JobName:       jn,
	}

	// Pin the latest version of the first get-step resource so the version
	// is locked at trigger time rather than at execution time.
	getSteps := j.GetSteps()
	if len(getSteps) > 0 {
		g := getSteps[0]
		rCan := g.ResourceCanonical()
		vers, err := q.Resources.FilterVersions(ctx, tc, pn, rCan)
		if err == nil && len(vers) > 0 {
			m.ResourceCanonical = rCan
			m.VersionID = vers[len(vers)-1].ID
		}
	}

	mb, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal Message Body: %w", err)
	}

	err = q.Topic.Send(ctx, &pubsub.Message{
		Body: mb,
	})
	if err != nil {
		return fmt.Errorf("failed to Trigger Job %q on Pipeline %q: %w", jn, pn, err)
	}

	return nil
}

func (q *PikoCI) GetPipelineJob(ctx context.Context, tc, pn, jn string) (*job.Job, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateCanonical(jn) {
		return nil, fmt.Errorf("invalid Job Name format %q", jn)
	}

	j, err := q.Jobs.Find(ctx, tc, pn, jn)
	if err != nil {
		return nil, fmt.Errorf("failed to Find Job %q on Pipeline %q: %w", jn, pn, err)
	}

	return j, nil
}
