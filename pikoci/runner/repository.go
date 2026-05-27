package runner

import "context"

//go:generate go tool mockgen -destination=../mock/runner_repository.go -mock_names=Repository=RunnerRepository -package mock github.com/xescugc/pikoci/pikoci/runner Repository

// Repository defines the persistence operations for runners.
type Repository interface {
	// Create persists a new runner in the given team and pipeline, returning the runner ID.
	Create(ctx context.Context, tc, pn string, ru Runner) (uint32, error)
	// Find retrieves a runner by team, pipeline, and runner name.
	Find(ctx context.Context, tc, pn, run string) (*Runner, error)
	// Filter returns all runners belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*Runner, error)
	// Update updates an existing runner identified by team, pipeline, and runner name.
	Update(ctx context.Context, tc, pn, run string, ru Runner) error
	// Delete removes a runner identified by team, pipeline, and runner name.
	Delete(ctx context.Context, tc, pn, run string) error
}
