package pikoci

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/scheduler"
	"github.com/xescugc/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
)

func (q *PikoCI) CreateResourceVersion(ctx context.Context, tc, pc, rCan string, v resource.Version) (*resource.Version, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	id, err := q.Resources.CreateVersion(ctx, tc, pc, rCan, v)
	if err != nil {
		return nil, fmt.Errorf("failed to Create Resource Version: %w", err)
	}

	v.ID = id

	return &v, nil
}

func (q *PikoCI) ListResourceVersions(ctx context.Context, tc, pc, rCan string) ([]*resource.Version, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	rvers, err := q.Resources.FilterVersions(ctx, tc, pc, rCan)
	if err != nil {
		return nil, fmt.Errorf("failed to List Resource Version: %w", err)
	}

	slices.Reverse(rvers)

	return rvers, nil
}

func (q *PikoCI) GetPipelineResource(ctx context.Context, tc, pc, rCan string) (*resource.Resource, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	r, err := q.Resources.Find(ctx, tc, pc, rCan)
	if err != nil {
		return nil, fmt.Errorf("failed to find Resource: %w", err)
	}

	return r, nil
}

func (q *PikoCI) UpdatePipelineResource(ctx context.Context, tc, pc, rCan string, r resource.Resource) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	err := q.Resources.Update(ctx, tc, pc, rCan, r)
	if err != nil {
		return fmt.Errorf("failed to update Resource: %w", err)
	}

	return nil
}

func (q *PikoCI) TriggerPipelineResource(ctx context.Context, tc, pc, rCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	r, err := q.Resources.Find(ctx, tc, pc, rCan)
	if err != nil {
		return fmt.Errorf("failed to find Resource: %w", err)
	}

	m := queue.Body{
		TeamCanonical:     tc,
		PipelineCanonical: pc,
		ResourceCanonical: rCan,
	}
	mb, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal Message Body: %w", err)
	}
	err = q.CheckTopic.Send(ctx, &pubsub.Message{
		Body: mb,
	})
	if err != nil {
		return fmt.Errorf("failed to Trigger Queue on Pipeline %q: %w", pc, err)
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
	_ = q.UpdatePipelineResource(ctx, tc, pc, r.Canonical, *r)

	return nil
}

func (q *PikoCI) WebhookTrigger(ctx context.Context, token string) error {
	r, tc, pc, err := q.Resources.FindByWebhookToken(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to find Resource by webhook token: %w", err)
	}

	return q.TriggerPipelineResource(ctx, tc, pc, r.Canonical)
}

func (q *PikoCI) RegenerateWebhookToken(ctx context.Context, tc, pc, rCan string) (string, error) {
	if !utils.ValidateCanonical(tc) {
		return "", fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return "", fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return "", fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	r, err := q.Resources.Find(ctx, tc, pc, rCan)
	if err != nil {
		return "", fmt.Errorf("failed to find Resource: %w", err)
	}

	r.WebhookToken = uuid.New().String()
	err = q.UpdatePipelineResource(ctx, tc, pc, rCan, *r)
	if err != nil {
		return "", fmt.Errorf("failed to update Resource: %w", err)
	}

	return r.WebhookToken, nil
}
