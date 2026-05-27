package sectype

import "context"

//go:generate go tool mockgen -destination=../mock/secret_type_repository.go -mock_names=Repository=SecretTypeRepository -package mock github.com/xescugc/pikoci/pikoci/sectype Repository

// Repository defines the persistence operations for secret types.
type Repository interface {
	// Create persists a new secret type in the given team and pipeline, returning its ID.
	Create(ctx context.Context, tc, pn string, st SecretType) (uint32, error)
	// Update updates an existing secret type identified by team, pipeline, and secret type name.
	Update(ctx context.Context, tc, pn, stn string, st SecretType) error
	// Find retrieves a secret type by team, pipeline, and secret type name.
	Find(ctx context.Context, tc, pn, stn string) (*SecretType, error)
	// Filter returns all secret types belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*SecretType, error)
	// Delete removes a secret type identified by team, pipeline, and secret type name.
	Delete(ctx context.Context, tc, pn, stn string) error
}
