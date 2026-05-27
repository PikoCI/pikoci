package pipeline

import (
	"context"
)

//go:generate go tool mockgen -destination=../mock/pipeline_repository.go -mock_names=Repository=PipelineRepository -package mock github.com/xescugc/pikoci/pikoci/pipeline Repository

// Repository defines the persistence operations for pipelines.
type Repository interface {
	// Create persists a new pipeline in the given team, returning the pipeline ID.
	Create(ctx context.Context, tc string, pp Pipeline) (uint32, error)
	// Update updates an existing pipeline identified by team and pipeline canonical.
	Update(ctx context.Context, tc, pCan string, pp Pipeline) error
	// Find retrieves a pipeline by team and pipeline canonical.
	Find(ctx context.Context, tc, pCan string) (*Pipeline, error)
	// FindPublic retrieves a public pipeline by team and pipeline canonical.
	FindPublic(ctx context.Context, tc, pCan string) (*Pipeline, error)
	// Filter returns all pipelines belonging to the given team.
	Filter(ctx context.Context, tc string) ([]*Pipeline, error)
	// FilterAll returns all pipelines across all teams, each paired with its team.
	FilterAll(ctx context.Context) ([]*WithTeam, error)
	// SetPublic updates the public visibility flag for a pipeline.
	SetPublic(ctx context.Context, tc, pCan string, public bool) error
	// Delete removes a pipeline identified by team and pipeline canonical.
	Delete(ctx context.Context, tc, pCan string) error
}
