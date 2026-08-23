package secret

import "context"

//go:generate go tool mockgen -destination=../mock/secret_repository.go -mock_names=Repository=SecretRepository -package mock github.com/pikoci/pikoci/pikoci/secret Repository

// Repository defines the persistence operations for the configuration store.
//
// Stored values are passed and returned as opaque bytes; the repository never
// encrypts or decrypts, and never inspects a value beyond its kind.
type Repository interface {
	// UpsertTeam stores a team-scoped entry, replacing any existing entry with
	// the same canonical name.
	UpsertTeam(ctx context.Context, tc string, e Entry, value []byte) (uint32, error)
	// UpsertPipeline stores a pipeline-scoped entry, replacing any existing
	// entry with the same canonical name.
	UpsertPipeline(ctx context.Context, tc, pn string, e Entry, value []byte) (uint32, error)
	// FilterTeam returns the team-scoped entries. Plain entries carry their
	// value; secret entries never do.
	FilterTeam(ctx context.Context, tc string) ([]*Entry, error)
	// FilterPipeline returns the pipeline-scoped entries. Plain entries carry
	// their value; secret entries never do.
	FilterPipeline(ctx context.Context, tc, pn string) ([]*Entry, error)
	// DeleteTeam removes a team-scoped entry by canonical name.
	DeleteTeam(ctx context.Context, tc, canonical string) error
	// DeletePipeline removes a pipeline-scoped entry by canonical name.
	DeletePipeline(ctx context.Context, tc, pn, canonical string) error
	// StoredValues returns the values visible to a pipeline, keyed by
	// canonical name. Team-scoped entries are included, with pipeline-scoped
	// entries shadowing team entries of the same name.
	StoredValues(ctx context.Context, tc, pn string) (map[string]StoredValue, error)

	// FindServerKey returns the stored wrapped identity, or nil when none has
	// been generated yet.
	FindServerKey(ctx context.Context) (*ServerKey, error)
	// CreateServerKey persists a newly generated wrapped identity. It fails if
	// one already exists, so concurrent servers cannot clobber each other.
	CreateServerKey(ctx context.Context, k ServerKey) error
}
