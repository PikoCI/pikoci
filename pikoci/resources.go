package pikoci

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/utils"
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

	if len(rvers) > 0 {
		versionIDs := make([]uint32, len(rvers))
		for i, v := range rvers {
			versionIDs[i] = v.ID
		}
		statuses, err := q.Builds.AggregateStatusByVersionIDs(ctx, versionIDs)
		if err != nil {
			q.logger.Warn("failed to fetch aggregate version statuses", "error", err)
		} else {
			for _, v := range rvers {
				if s, ok := statuses[v.ID]; ok {
					v.Status = s
				}
			}
		}
	}

	return rvers, hasMore, nil
}

// ListPipelineResources returns all resources for the given pipeline,
// enriched with the latest version and its aggregate build status.
func (q *PikoCI) ListPipelineResources(ctx context.Context, tc, pc string) ([]*resource.Resource, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return nil, fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	}

	rs, err := q.Resources.Filter(ctx, tc, pc)
	if err != nil {
		return nil, fmt.Errorf("failed to list Pipeline Resources: %w", err)
	}

	// Fetch latest version for each resource
	var versionIDs []uint32
	for _, r := range rs {
		vers, err := q.Resources.FilterVersions(ctx, tc, pc, r.Canonical, nil, nil, 1)
		if err != nil || len(vers) == 0 {
			continue
		}
		r.LatestVersion = vers[0]
		versionIDs = append(versionIDs, vers[0].ID)
	}

	// Batch fetch aggregate build statuses for all latest versions
	if len(versionIDs) > 0 {
		statuses, err := q.Builds.AggregateStatusByVersionIDs(ctx, versionIDs)
		if err != nil {
			q.logger.Warn("failed to fetch aggregate version statuses", "error", err)
		} else {
			for _, r := range rs {
				if r.LatestVersion == nil {
					continue
				}
				if s, ok := statuses[r.LatestVersion.ID]; ok {
					r.LatestVersion.Status = s
				}
			}
		}
	}

	return rs, nil
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

// PinResourceVersion pins a resource to a specific version, preventing the
// scheduler from using newer versions. It also cancels any pending builds
// for downstream jobs that reference this resource with a different version,
// ensuring that stale versions are not executed after the pin takes effect.
func (q *PikoCI) PinResourceVersion(ctx context.Context, tc, pc, rCan string, versionID uint32) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	if err := q.validateResourceVersion(ctx, tc, pc, rCan, versionID); err != nil {
		return err
	}

	if err := q.Resources.PinVersion(ctx, tc, pc, rCan, versionID); err != nil {
		return err
	}

	// Cancel pending builds that reference a different version of this resource.
	q.cancelMismatchedPendingBuilds(ctx, tc, pc, rCan, versionID)

	return nil
}

// UnpinResourceVersion removes the version pin from a resource, allowing the
// scheduler to use newer versions again.
func (q *PikoCI) UnpinResourceVersion(ctx context.Context, tc, pc, rCan string) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	return q.Resources.UnpinVersion(ctx, tc, pc, rCan)
}

// TriggerResourceVersion triggers immediate downstream jobs (those with get
// steps that have no passed constraints) with a specific resource version.
func (q *PikoCI) TriggerResourceVersion(ctx context.Context, tc, pc, rCan string, versionID uint32) error {
	if !utils.ValidateCanonical(tc) {
		return fmt.Errorf("invalid Team Canonical format %q", tc)
	} else if !utils.ValidateCanonical(pc) {
		return fmt.Errorf("invalid Pipeline Canonical format %q", pc)
	} else if !utils.ValidateResourceCanonical(rCan) {
		return fmt.Errorf("invalid Resource Canonical format %q", rCan)
	}

	if err := q.validateResourceVersion(ctx, tc, pc, rCan, versionID); err != nil {
		return err
	}

	p, err := q.Pipelines.Find(ctx, tc, pc)
	if err != nil {
		return fmt.Errorf("failed to find Pipeline %q: %w", pc, err)
	}

	for _, j := range p.Jobs {
		if j.Paused {
			continue
		}
		for _, ps := range j.FlatPlanSteps() {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			g := ps.Get
			if g.ResourceCanonical() != rCan || len(g.Passed) != 0 {
				continue
			}

			bb := build.Build{
				Status:            build.Pending,
				VersionID:         versionID,
				ResourceCanonical: rCan,
			}

			_, err := q.CreateJobBuild(ctx, tc, pc, j.Name, bb)
			if err != nil {
				return fmt.Errorf("failed to create pending build for Job %q: %w", j.Name, err)
			}

			break // only trigger once per job
		}
	}

	q.Notifier.Notify()

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

	// Set NextCheck to now so FilterDueResources returns it immediately.
	// NextWork will update LastCheck and NextCheck when it claims the check.
	r.NextCheck = time.Now()
	_ = q.UpdatePipelineResource(ctx, tc, pc, r.Canonical, *r)

	q.Notifier.Notify()

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

	r.WebhookToken, err = newWebhookToken(r.Canonical)
	if err != nil {
		return "", fmt.Errorf("failed to generate webhook token: %w", err)
	}
	err = q.UpdatePipelineResource(ctx, tc, pc, rCan, *r)
	if err != nil {
		return "", fmt.Errorf("failed to update Resource: %w", err)
	}

	return r.WebhookToken, nil
}

// newWebhookToken generates a webhook token that embeds the resource canonical
// for readability: "canonical_uuid". The result is capped at 255 chars (DB limit).
func newWebhookToken(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("webhook token name must not be empty")
	}
	id := uuid.New().String()
	const maxLen = 255
	maxName := maxLen - len(id) - 1 // 218
	if len(name) > maxName {
		name = name[:maxName]
	}
	return name + "_" + id, nil
}

// validateResourceVersion checks that the given versionID belongs to the
// specified resource, returning an error if it does not.
func (q *PikoCI) validateResourceVersion(ctx context.Context, tc, pc, rCan string, versionID uint32) error {
	vers, err := q.Resources.FilterVersions(ctx, tc, pc, rCan, nil, nil, 0)
	if err != nil {
		return fmt.Errorf("failed to list versions for resource %q: %w", rCan, err)
	}
	for _, v := range vers {
		if v.ID == versionID {
			return nil
		}
	}
	return fmt.Errorf("version %d does not belong to resource %q", versionID, rCan)
}

// cancelMismatchedPendingBuilds finds all jobs that use the given resource and
// cancels any of their pending builds whose version_id differs from the pinned
// version. This is best-effort; errors are logged but do not fail the pin operation.
func (q *PikoCI) cancelMismatchedPendingBuilds(ctx context.Context, tc, pc, rCan string, pinnedVersionID uint32) {
	p, err := q.Pipelines.Find(ctx, tc, pc)
	if err != nil {
		return
	}

	for _, j := range p.Jobs {
		// Check if this job uses the pinned resource in any get step.
		usesResource := false
		for _, ps := range j.FlatPlanSteps() {
			if ps.Type == job.StepTypeGet && ps.Get != nil && ps.Get.ResourceCanonical() == rCan {
				usesResource = true
				break
			}
		}
		if !usesResource {
			continue
		}

		// Fetch all builds for this job and cancel pending ones with wrong version.
		builds, err := q.Builds.Filter(ctx, tc, pc, j.Name, nil, nil, 0)
		if err != nil {
			continue
		}
		for _, b := range builds {
			if b.Status == build.Pending && b.ResourceCanonical == rCan && b.VersionID != pinnedVersionID {
				b.Status = build.Cancelled
				_ = q.Builds.Update(ctx, tc, pc, j.Name, b.BuildNumber, *b)
			}
		}
	}
}
