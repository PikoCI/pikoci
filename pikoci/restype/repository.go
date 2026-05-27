package restype

import "context"

//go:generate go tool mockgen -destination=../mock/resource_type_repository.go -mock_names=Repository=ResourceTypeRepository -package mock github.com/xescugc/pikoci/pikoci/restype Repository

// Repository defines the persistence operations for resource types.
type Repository interface {
	// Create persists a new resource type in the given team and pipeline, returning its ID.
	Create(ctx context.Context, tc, pn string, rt ResourceType) (uint32, error)
	// Update updates an existing resource type identified by team, pipeline, and type name.
	Update(ctx context.Context, tc, pn, tn string, rt ResourceType) error
	// Find retrieves a resource type by team, pipeline, and type name.
	Find(ctx context.Context, tc, pn, tn string) (*ResourceType, error)
	// Filter returns all resource types belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*ResourceType, error)
	// Delete removes a resource type identified by team, pipeline, and type name.
	Delete(ctx context.Context, tc, pn, tn string) error
}
