package secret

import "context"

//go:generate go tool mockgen -destination=../mock/secret_repository.go -mock_names=Repository=SecretRepository -package mock github.com/xescugc/pikoci/pikoci/secret Repository

// Repository defines the persistence operations for secrets.
type Repository interface {
	// Create persists a new secret in the given team and pipeline, returning the secret ID.
	Create(ctx context.Context, tc, pn string, s Secret) (uint32, error)
	// Update updates an existing secret identified by team, pipeline, and secret canonical.
	Update(ctx context.Context, tc, pn, sCan string, s Secret) error
	// Find retrieves a secret by team, pipeline, and secret canonical.
	Find(ctx context.Context, tc, pn, sCan string) (*Secret, error)
	// Filter returns all secrets belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*Secret, error)
	// Delete removes a secret identified by team, pipeline, and secret canonical.
	Delete(ctx context.Context, tc, pn, sCan string) error
}
