package mysql_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
)

func TestOAuthProviderRepository_CRUD(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewOAuthProviderRepository(db)
	ctx := context.Background()

	// Create
	id, err := repo.CreateProvider(ctx, oauthprovider.Provider{
		Name: "GitHub", Canonical: "github", Type: "oauth2",
		AuthURL: "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
		UserinfoURL: "https://api.github.com/user",
		Scopes: "user:email", ClientID: "client-id", ClientSecret: "client-secret",
		UsernameClaim: "login", Enabled: true,
	})
	require.NoError(t, err)
	assert.True(t, id > 0)

	// FindByCanonical
	p, err := repo.FindProviderByCanonical(ctx, "github")
	require.NoError(t, err)
	assert.Equal(t, "GitHub", p.Name)
	assert.Equal(t, "github", p.Canonical)
	assert.Equal(t, "oauth2", p.Type)
	assert.Equal(t, "client-id", p.ClientID)
	assert.True(t, p.Enabled)

	// FindProvider by ID
	p2, err := repo.FindProvider(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, p.Canonical, p2.Canonical)

	// FilterProviders
	all, err := repo.FilterProviders(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	// FilterEnabledProviders
	enabled, err := repo.FilterEnabledProviders(ctx)
	require.NoError(t, err)
	assert.Len(t, enabled, 1)

	// Update
	err = repo.UpdateProvider(ctx, "github", oauthprovider.Provider{
		Name: "GitHub Updated", Canonical: "github", Type: "oauth2",
		AuthURL: "https://github.com/login/oauth/authorize",
		TokenURL: "https://github.com/login/oauth/access_token",
		UserinfoURL: "https://api.github.com/user",
		Scopes: "user:email", ClientID: "new-client-id", ClientSecret: "new-secret",
		UsernameClaim: "login", Enabled: false,
	})
	require.NoError(t, err)

	// Verify update
	p, err = repo.FindProviderByCanonical(ctx, "github")
	require.NoError(t, err)
	assert.Equal(t, "GitHub Updated", p.Name)
	assert.Equal(t, "new-client-id", p.ClientID)
	assert.False(t, p.Enabled)

	// FilterEnabledProviders after disable
	enabled, err = repo.FilterEnabledProviders(ctx)
	require.NoError(t, err)
	assert.Len(t, enabled, 0)

	// Delete
	err = repo.DeleteProvider(ctx, "github")
	require.NoError(t, err)

	all, err = repo.FilterProviders(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 0)
}

func TestOAuthProviderRepository_UserLinks(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewOAuthProviderRepository(db)
	ur := mysql.NewUserRepository(db)
	ctx := context.Background()

	// Create provider
	pid, err := repo.CreateProvider(ctx, oauthprovider.Provider{
		Name: "GitHub", Canonical: "github", Type: "oauth2",
		AuthURL: "http://x", TokenURL: "http://x", UserinfoURL: "http://x",
		Scopes: "x", ClientID: "x", ClientSecret: "x",
		UsernameClaim: "login", Enabled: true,
	})
	require.NoError(t, err)

	// Use admin user from migration (ID=1)
	_ = ur
	userID := uint32(1)

	// CreateUserLink
	linkID, err := repo.CreateUserLink(ctx, oauthprovider.UserLink{
		UserID: userID, ProviderID: pid, Subject: "github-user-123", Email: "user@example.com",
	})
	require.NoError(t, err)
	assert.True(t, linkID > 0)

	// FindUserLink
	link, err := repo.FindUserLink(ctx, pid, "github-user-123")
	require.NoError(t, err)
	assert.Equal(t, userID, link.UserID)
	assert.Equal(t, "user@example.com", link.Email)

	// FindUserLinksByUser
	links, err := repo.FindUserLinksByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, links, 1)

	// DeleteUserLinkByUserAndProvider
	err = repo.DeleteUserLinkByUserAndProvider(ctx, userID, pid)
	require.NoError(t, err)

	links, err = repo.FindUserLinksByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, links, 0)

	// Create again and delete by ID
	linkID, err = repo.CreateUserLink(ctx, oauthprovider.UserLink{
		UserID: userID, ProviderID: pid, Subject: "github-user-456",
	})
	require.NoError(t, err)

	err = repo.DeleteUserLink(ctx, linkID)
	require.NoError(t, err)
}

func TestOAuthProviderRepository_AuthSettings(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewOAuthProviderRepository(db)
	ctx := context.Background()

	// Get default settings (seeded by migration)
	settings, err := repo.GetAuthSettings(ctx)
	require.NoError(t, err)
	assert.True(t, settings.LocalAuthEnabled)

	// Update
	err = repo.UpdateAuthSettings(ctx, oauthprovider.AuthSettings{
		ID: settings.ID, LocalAuthEnabled: false,
	})
	require.NoError(t, err)

	// Verify
	settings, err = repo.GetAuthSettings(ctx)
	require.NoError(t, err)
	assert.False(t, settings.LocalAuthEnabled)
}

func TestOAuthProviderRepository_CascadeDelete(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewOAuthProviderRepository(db)
	ctx := context.Background()

	// Create provider and link
	pid, _ := repo.CreateProvider(ctx, oauthprovider.Provider{
		Name: "Test", Canonical: "test", Type: "oauth2",
		AuthURL: "http://x", TokenURL: "http://x", UserinfoURL: "http://x",
		Scopes: "x", ClientID: "x", ClientSecret: "x",
		UsernameClaim: "email", Enabled: true,
	})
	_, err := repo.CreateUserLink(ctx, oauthprovider.UserLink{
		UserID: 1, ProviderID: pid, Subject: "sub-1",
	})
	require.NoError(t, err)

	// Delete provider — link should cascade
	err = repo.DeleteProvider(ctx, "test")
	require.NoError(t, err)

	links, err := repo.FindUserLinksByUser(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, links, 0)
}

func TestOAuthProviderRepository_FindByID(t *testing.T) {
	db := setupTestDB(t)
	ur := mysql.NewUserRepository(db)
	ctx := context.Background()

	// FindByID using migration-seeded admin
	u, err := ur.FindByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "admin", u.Username)
}
