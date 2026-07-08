package oauthprovider

import "context"

//go:generate go tool mockgen -destination=../mock/oauth_provider_repository.go -mock_names=Repository=OAuthProviderRepository -package mock github.com/pikoci/pikoci/pikoci/oauthprovider Repository

// Repository defines the persistence operations for OAuth providers.
type Repository interface {
	// CreateProvider persists a new OAuth provider, returning the provider ID.
	CreateProvider(ctx context.Context, p Provider) (uint32, error)
	// FindProvider retrieves a provider by ID.
	FindProvider(ctx context.Context, id uint32) (*Provider, error)
	// FindProviderByCanonical retrieves a provider by canonical name.
	FindProviderByCanonical(ctx context.Context, canonical string) (*Provider, error)
	// FilterProviders returns all providers.
	FilterProviders(ctx context.Context) ([]*Provider, error)
	// FilterEnabledProviders returns only enabled providers.
	FilterEnabledProviders(ctx context.Context) ([]*Provider, error)
	// UpdateProvider updates an existing provider identified by canonical.
	UpdateProvider(ctx context.Context, canonical string, p Provider) error
	// DeleteProvider removes a provider by canonical.
	DeleteProvider(ctx context.Context, canonical string) error

	// CreateUserLink creates a link between a user and a provider identity.
	CreateUserLink(ctx context.Context, link UserLink) (uint32, error)
	// FindUserLink finds a link by provider ID and subject.
	FindUserLink(ctx context.Context, providerID uint32, subject string) (*UserLink, error)
	// FindUserLinksByUser returns all links for a given user.
	FindUserLinksByUser(ctx context.Context, userID uint32) ([]*UserLink, error)
	// DeleteUserLink removes a user link by ID.
	DeleteUserLink(ctx context.Context, id uint32) error
	// DeleteUserLinkByUserAndProvider removes a link by user ID and provider ID.
	DeleteUserLinkByUserAndProvider(ctx context.Context, userID uint32, providerID uint32) error

	// GetAuthSettings returns the global auth settings.
	GetAuthSettings(ctx context.Context) (*AuthSettings, error)
	// UpdateAuthSettings updates the global auth settings.
	UpdateAuthSettings(ctx context.Context, settings AuthSettings) error
}
