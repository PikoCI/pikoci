package secret

import "context"

//go:generate go tool mockgen -destination=../mock/secret_repository.go -mock_names=Repository=SecretRepository -package mock github.com/pikoci/pikoci/pikoci/secret Repository

// Repository defines the persistence operations for stored secrets.
//
// Encrypted values are passed and returned as opaque byte slices; the
// repository never sees plaintext.
type Repository interface {
	// UpsertTeam stores an encrypted value for a team-scoped secret, replacing
	// any existing secret with the same canonical name.
	UpsertTeam(ctx context.Context, tc string, s Secret, value []byte) (uint32, error)
	// UpsertPipeline stores an encrypted value for a pipeline-scoped secret,
	// replacing any existing secret with the same canonical name.
	UpsertPipeline(ctx context.Context, tc, pn string, s Secret, value []byte) (uint32, error)
	// FilterTeam returns the team-scoped secrets, without their values.
	FilterTeam(ctx context.Context, tc string) ([]*Secret, error)
	// FilterPipeline returns the pipeline-scoped secrets, without their values.
	FilterPipeline(ctx context.Context, tc, pn string) ([]*Secret, error)
	// DeleteTeam removes a team-scoped secret by canonical name.
	DeleteTeam(ctx context.Context, tc, sCan string) error
	// DeletePipeline removes a pipeline-scoped secret by canonical name.
	DeletePipeline(ctx context.Context, tc, pn, sCan string) error
	// EncryptedValues returns the encrypted values visible to a pipeline, keyed
	// by canonical name. Team-scoped secrets are included, with pipeline-scoped
	// secrets shadowing team entries of the same name.
	EncryptedValues(ctx context.Context, tc, pn string) (map[string][]byte, error)

	// FindServerKey returns the stored wrapped identity, or nil when none has
	// been generated yet.
	FindServerKey(ctx context.Context) (*ServerKey, error)
	// CreateServerKey persists a newly generated wrapped identity. It fails if
	// one already exists, so concurrent servers cannot clobber each other.
	CreateServerKey(ctx context.Context, k ServerKey) error
}
