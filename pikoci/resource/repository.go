package resource

import "context"

//go:generate go tool mockgen -destination=../mock/resource_repository.go -mock_names=Repository=ResourceRepository -package mock github.com/pikoci/pikoci/pikoci/resource Repository

// Repository defines the persistence operations for resources and their versions.
type Repository interface {
	// Create persists a new resource in the given team and pipeline, returning the resource ID.
	Create(ctx context.Context, tc, pn string, r Resource) (uint32, error)
	// Update updates an existing resource identified by team, pipeline, and resource canonical.
	Update(ctx context.Context, tc, pn, rCan string, r Resource) error
	// Find retrieves a resource by team, pipeline, and resource canonical.
	Find(ctx context.Context, tc, pn, rCan string) (*Resource, error)
	// FindByWebhookToken retrieves a resource by its webhook token, also returning the team and pipeline canonicals.
	FindByWebhookToken(ctx context.Context, token string) (*Resource, string, string, error)
	// Filter returns all resources belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*Resource, error)
	// FilterDueResources returns all resources whose next check time has passed, across all pipelines.
	FilterDueResources(ctx context.Context) ([]*ResourceWithPipeline, error)
	// Delete removes a resource identified by team, pipeline, and resource canonical.
	Delete(ctx context.Context, tc, pn, rCan string) error

	// CreateVersion persists a new version for the given resource, returning the version ID.
	CreateVersion(ctx context.Context, tc, pn, rCan string, v Version) (uint32, error)
	// FilterVersions returns a paginated list of versions for the given resource.
	FilterVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*Version, error)
}

// ResourceWithPipeline embeds a Resource along with its owning team and pipeline canonicals.
type ResourceWithPipeline struct {
	Resource
	TeamCanonical     string
	PipelineCanonical string
}
