package pikoci

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/scheduler"
	"github.com/pikoci/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
)

func (q *PikoCI) CreateResourceVersion(ctx context.Context, tc, pn, rCan string, v resource.Version) (*resource.Version, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	id, err := q.Resources.CreateVersion(ctx, tc, pn, rCan, v)
	if err != nil {
		return nil, fmt.Errorf("failed to Create Resource Version: %w", err)
	}

	v.ID = id

	return &v, nil
}

func (q *PikoCI) ListResourceVersions(ctx context.Context, tc, pn, rCan string) ([]*resource.Version, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	rvers, err := q.Resources.FilterVersions(ctx, tc, pn, rCan)
	if err != nil {
		return nil, fmt.Errorf("failed to List Resource Version: %w", err)
	}

	slices.Reverse(rvers)

	return rvers, nil
}

func (q *PikoCI) GetPipelineResource(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return nil, fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	r, err := q.Resources.Find(ctx, tc, pn, rCan)
	if err != nil {
		return nil, fmt.Errorf("failed to find Resource: %w", err)
	}

	return r, nil
}

func (q *PikoCI) UpdatePipelineResource(ctx context.Context, tc, pn, rCan string, r resource.Resource) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	err := q.Resources.Update(ctx, tc, pn, rCan, r)
	if err != nil {
		return fmt.Errorf("failed to update Resource: %w", err)
	}

	return nil
}

func (q *PikoCI) TriggerPipelineResource(ctx context.Context, tc, pn, rCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	r, err := q.Resources.Find(ctx, tc, pn, rCan)
	if err != nil {
		return fmt.Errorf("failed to find Resource: %w", err)
	}

	m := queue.Body{
		TeamCanonical:     tc,
		PipelineName:      pn,
		ResourceCanonical: rCan,
	}
	mb, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal Message Body: %w", err)
	}
	err = q.Topic.Send(ctx, &pubsub.Message{
		Body: mb,
	})
	if err != nil {
		return fmt.Errorf("failed to Trigger Queue on Pipeline %q: %w", pn, err)
	}
	now := time.Now()
	r.LastCheck = now
	spec := r.CheckInterval
	if spec == "" {
		spec = "@every 1m"
	}
	nextCheck, err := scheduler.ComputeNextCheck(spec, now)
	if err == nil {
		r.NextCheck = nextCheck
	}
	_ = q.UpdatePipelineResource(ctx, tc, pn, r.Canonical, *r)

	return nil
}

func (q *PikoCI) WebhookTrigger(ctx context.Context, token string) error {
	r, tc, pn, err := q.Resources.FindByWebhookToken(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to find Resource by webhook token: %w", err)
	}

	return q.TriggerPipelineResource(ctx, tc, pn, r.Canonical)
}

func (q *PikoCI) RegenerateWebhookToken(ctx context.Context, tc, pn, rCan string) (string, error) {
	if !utils.ValidateCanonical(tc) {
		return "", fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pn) {
		return "", fmt.Errorf("invalid Pipeline Name format %q", pn)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return "", fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	r, err := q.Resources.Find(ctx, tc, pn, rCan)
	if err != nil {
		return "", fmt.Errorf("failed to find Resource: %w", err)
	}

	r.WebhookToken = uuid.New().String()
	err = q.UpdatePipelineResource(ctx, tc, pn, rCan, *r)
	if err != nil {
		return "", fmt.Errorf("failed to update Resource: %w", err)
	}

	return r.WebhookToken, nil
}
