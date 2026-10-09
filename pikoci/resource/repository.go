package resource

import (
	"context"
	"errors"
	"time"
)

//go:generate go tool mockgen -destination=../mock/resource_repository.go -mock_names=Repository=ResourceRepository -package mock github.com/pikoci/pikoci/pikoci/resource Repository

// Repository defines the persistence operations for resources and their versions.
type Repository interface {
	// Create persists a new resource in the given team and pipeline, returning the resource ID.
	Create(ctx context.Context, tc, pn string, r Resource) (uint32, error)
	// Update updates an existing resource identified by team, pipeline, and resource canonical.
	Update(ctx context.Context, tc, pn, rCan string, r Resource) error
	// UpdateLogs replaces only the resource's check logs, leaving its
	// configuration untouched.
	UpdateLogs(ctx context.Context, tc, pn, rCan, logs string) error
	// Find retrieves a resource by team, pipeline, and resource canonical.
	Find(ctx context.Context, tc, pn, rCan string) (*Resource, error)
	// FindByWebhookToken retrieves a resource by its webhook token, also returning the team and pipeline canonicals.
	FindByWebhookToken(ctx context.Context, token string) (*Resource, string, string, error)
	// Filter returns all resources belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*Resource, error)
	// FilterDueResources returns all resources whose next check time has passed, across all pipelines.
	FilterDueResources(ctx context.Context) ([]*ResourceWithPipeline, error)
	// ClaimResourceCheck atomically updates a due resource's LastCheck and NextCheck,
	// returning true if this caller won the claim. Uses optimistic locking on next_check
	// to prevent two workers from processing the same check. The claim only
	// succeeds while the resource's retrigger version still equals
	// retriggerVersionID (as read by FilterDueResources), and clears it, so a
	// re-trigger request is handed to exactly one worker.
	ClaimResourceCheck(ctx context.Context, tc, pn, rCan string, prevNextCheck time.Time, newLastCheck, newNextCheck time.Time, retriggerVersionID uint32) (bool, error)
	// RequestCheck makes a resource due at the given time and marks the check
	// as requested, so FilterDueResources returns it ahead of scheduled checks.
	// The mark is cleared when the check is claimed.
	RequestCheck(ctx context.Context, tc, pn, rCan string, at time.Time) error
	// RequestRetrigger is RequestCheck for a manual re-trigger of versionID:
	// the worker that claims the check runs the on_trigger hooks and creates
	// the builds for that version, then runs the check. It returns
	// ErrRetriggerPending while a different version is still waiting for a
	// worker; asking again for the same version is a no-op.
	RequestRetrigger(ctx context.Context, tc, pn, rCan string, versionID uint32, at time.Time) error
	// PinVersion pins a resource to a specific version, preventing the scheduler from using newer versions.
	PinVersion(ctx context.Context, tc, pn, rCan string, versionID uint32) error
	// UnpinVersion removes the version pin from a resource, allowing the scheduler to use newer versions.
	UnpinVersion(ctx context.Context, tc, pn, rCan string) error
	// Delete removes a resource identified by team, pipeline, and resource canonical.
	Delete(ctx context.Context, tc, pn, rCan string) error

	// CreateVersion persists a new version for the given resource, returning the version ID.
	CreateVersion(ctx context.Context, tc, pn, rCan string, v Version) (uint32, error)
	// FilterVersions returns a paginated list of versions for the given resource.
	FilterVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*Version, error)
	// FindVersionByID retrieves a single version by its ID, also returning
	// the resource canonical it belongs to.
	FindVersionByID(ctx context.Context, versionID uint32) (*Version, string, error)
	// LatestVersionByResources returns the latest version for each resource in a pipeline.
	// The key is resource canonical → latest version.
	LatestVersionByResources(ctx context.Context, tc, pn string) (map[string]*Version, error)
}

// ResourceWithPipeline embeds a Resource along with its owning team and pipeline canonicals.
// ErrRetriggerPending is returned by RequestRetrigger while another version
// of the resource is still waiting to be re-triggered by a worker.
var ErrRetriggerPending = errors.New("another version of this resource is already waiting to be re-triggered, try again once it has started")

type ResourceWithPipeline struct {
	Resource
	TeamCanonical     string
	PipelineCanonical string
	// RetriggerVersionID is the version a manual re-trigger asked for, or 0.
	RetriggerVersionID uint32
}
