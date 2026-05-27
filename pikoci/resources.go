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

// CreateResourceVersion creates a new version for the specified resource.
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

// ListResourceVersions returns paginated versions for a resource, supporting
// cursor-based pagination with before and after parameters. Results are returned
// in newest-first order.
func (q *PikoCI) ListResourceVersions(ctx context.Context, tc, pc, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, bool, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, false, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, false, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return nil, false, fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	fetchLimit := limit
	if limit > 0 {
		fetchLimit = limit + 1
	}

	rvers, err := q.Resources.FilterVersions(ctx, tc, pc, rCan, before, after, fetchLimit)
	if err != nil {
		return nil, false, fmt.Errorf("failed to List Resource Version: %w", err)
	}

	hasMore := false
	if limit > 0 && uint32(len(rvers)) > limit {
		hasMore = true
		rvers = rvers[:limit]
	}

	// For "after" queries the DB returns ASC order; reverse to newest-first
	if after != nil {
		slices.Reverse(rvers)
	}

	return rvers, hasMore, nil
}

// GetPipelineResource retrieves a resource by its canonical name within a pipeline.
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

// UpdatePipelineResource updates a resource's metadata within a pipeline.
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

// TriggerPipelineResource enqueues a resource check via the check topic and
// updates the resource's last check time and next scheduled check time.
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

// WebhookTrigger triggers a resource check using the resource's unique webhook
// token. It looks up the resource by token and delegates to TriggerPipelineResource.
func (q *PikoCI) WebhookTrigger(ctx context.Context, token string) error {
	r, tc, pc, err := q.Resources.FindByWebhookToken(ctx, token)
	if err != nil {
		return fmt.Errorf("failed to find Resource by webhook token: %w", err)
	}

	return q.TriggerPipelineResource(ctx, tc, pc, r.Canonical)
}

// RegenerateWebhookToken generates a new webhook token for the specified resource,
// invalidating the previous one.
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
