package pikoci_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newServiceWithOAuth creates a MockService with a mock OAuthProviderRepository
// wired into the PikoCI instance (unlike the default newService which passes nil).
func newServiceWithOAuth(ctrl *gomock.Controller) (MockService, *mock.OAuthProviderRepository) {
	ur := mock.NewUserRepository(ctrl)
	tr := mock.NewTeamRepository(ctrl)
	pr := mock.NewPipelineRepository(ctrl)
	jr := mock.NewJobRepository(ctrl)
	rr := mock.NewResourceRepository(ctrl)
	rtr := mock.NewResourceTypeRepository(ctrl)
	br := mock.NewBuildRepository(ctrl)
	rur := mock.NewRunnerRepository(ctrl)
	str := mock.NewSecretTypeRepository(ctrl)
	tgr := mock.NewTriggerRepository(ctrl)
	ntr := mock.NewNotificationTypeRepository(ctrl)
	nr := mock.NewNotificationRepository(ctrl)
	wr := mock.NewWorkerRepository(ctrl)
	atr := mock.NewApiTokenRepository(ctrl)
	opr := mock.NewOAuthProviderRepository(ctrl)

	suow := unitwork.NewNoopStartUnitOfWork(unitwork.Repositories{
		UsersRepo:             ur,
		TeamsRepo:             tr,
		PipelinesRepo:         pr,
		JobsRepo:              jr,
		ResourcesRepo:         rr,
		ResourceTypesRepo:     rtr,
		BuildsRepo:            br,
		RunnersRepo:           rur,
		SecretTypesRepo:       str,
		NotificationTypesRepo: ntr,
		NotificationsRepo:     nr,
		ApiTokensRepo:         atr,
	})

	alr := mock.NewAuditLogRepository(ctrl)
	alr.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	p := pikoci.New(context.TODO(), ur, tr, pr, jr, rr, rtr, br, rur, str, tgr, wr, atr, alr, opr, suow, []byte("test-secret"), nil, nil)
	ms := MockService{
		Users:             ur,
		Teams:             tr,
		Pipelines:         pr,
		Jobs:              jr,
		Resources:         rr,
		ResourceTypes:     rtr,
		Builds:            br,
		Runners:           rur,
		SecretTypes:       str,
		Triggers:          tgr,
		NotificationTypes: ntr,
		Notifications:     nr,
		Workers:           wr,
		ApiTokens:         atr,
		AuditLogs:         alr,

		S: p,
		P: p,
	}
	return ms, opr
}

func TestGetAuthMethods_LocalEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: true,
	}, nil)
	opr.EXPECT().FilterEnabledProviders(ctx).Return([]*oauthprovider.Provider{
		{Canonical: "github", Name: "GitHub", Type: "oauth2"},
		{Canonical: "google", Name: "Google", Type: "oidc"},
	}, nil)

	methods, err := s.S.GetAuthMethods(ctx)
	require.NoError(t, err)
	require.NotNil(t, methods)
	assert.True(t, methods.LocalAuthEnabled)
	assert.Len(t, methods.Providers, 2)
	assert.Equal(t, "github", methods.Providers[0].Canonical)
	assert.Equal(t, "GitHub", methods.Providers[0].Name)
	assert.Equal(t, "oauth2", methods.Providers[0].Type)
	assert.Equal(t, "google", methods.Providers[1].Canonical)
}

func TestGetAuthMethods_LocalDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: false,
	}, nil)
	opr.EXPECT().FilterEnabledProviders(ctx).Return([]*oauthprovider.Provider{
		{Canonical: "github", Name: "GitHub", Type: "oauth2"},
	}, nil)

	methods, err := s.S.GetAuthMethods(ctx)
	require.NoError(t, err)
	require.NotNil(t, methods)
	assert.False(t, methods.LocalAuthEnabled)
	assert.Len(t, methods.Providers, 1)
}

func TestGetAuthMethods_NilRepository(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	// Simulate repository being unavailable by returning an error
	opr.EXPECT().GetAuthSettings(ctx).Return(nil, fmt.Errorf("repository not available"))

	methods, err := s.S.GetAuthMethods(ctx)
	require.NoError(t, err)
	require.NotNil(t, methods)
	assert.True(t, methods.LocalAuthEnabled)
}

func TestCreateOAuthProvider_Valid(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	input := oauthprovider.Provider{
		Name:         "My OIDC",
		Type:         "oidc",
		IssuerURL:    "https://accounts.google.com",
		ClientID:     "client-id-123",
		ClientSecret: "client-secret-456",
	}

	opr.EXPECT().CreateProvider(ctx, gomock.Any()).Return(uint32(1), nil)

	p, err := s.S.CreateOAuthProvider(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, uint32(1), p.ID)
	assert.Equal(t, "My OIDC", p.Name)
	assert.Equal(t, "my-oidc", p.Canonical)
	assert.Equal(t, "oidc", p.Type)
	assert.Equal(t, "openid email profile", p.Scopes)
	assert.Equal(t, "email", p.UsernameClaim)
}

func TestCreateOAuthProvider_MissingName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	input := oauthprovider.Provider{
		Type:         "oidc",
		IssuerURL:    "https://accounts.google.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}

	_, err := s.S.CreateOAuthProvider(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name is required")
}

func TestCreateOAuthProvider_InvalidType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	input := oauthprovider.Provider{
		Name:         "Bad Provider",
		Type:         "saml",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}

	_, err := s.S.CreateOAuthProvider(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider type must be 'oidc' or 'oauth2'")
}

func TestCreateOAuthProvider_OIDCMissingIssuerURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	input := oauthprovider.Provider{
		Name:         "No Issuer",
		Type:         "oidc",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}

	_, err := s.S.CreateOAuthProvider(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "issuer_url is required for OIDC providers")
}

func TestCreateOAuthProvider_OAuth2MissingURLs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	input := oauthprovider.Provider{
		Name:         "No URLs",
		Type:         "oauth2",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	}

	_, err := s.S.CreateOAuthProvider(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_url and token_url are required for OAuth2 providers")
}

func TestUpdateAuthSettings_CannotDisableWithoutProviders(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: true,
	}, nil)
	opr.EXPECT().FilterEnabledProviders(ctx).Return([]*oauthprovider.Provider{}, nil)

	err := s.S.UpdateAuthSettings(ctx, oauthprovider.AuthSettings{LocalAuthEnabled: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot disable local auth")
}

func TestUpdateAuthSettings_CanDisableWithProviders(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: true,
	}, nil)
	opr.EXPECT().FilterEnabledProviders(ctx).Return([]*oauthprovider.Provider{
		{ID: 1, Canonical: "github", Name: "GitHub", Enabled: true},
	}, nil)
	// Admin user with a linked OAuth account
	s.Users.EXPECT().Filter(ctx).Return([]*user.User{
		{ID: 1, Username: "admin", Admin: true},
	}, nil)
	opr.EXPECT().FindUserLinksByUser(ctx, uint32(1)).Return([]*oauthprovider.UserLink{
		{ID: 1, UserID: 1, ProviderID: 1, Subject: "sub-123"},
	}, nil)
	opr.EXPECT().UpdateAuthSettings(ctx, oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: false,
	}).Return(nil)

	err := s.S.UpdateAuthSettings(ctx, oauthprovider.AuthSettings{LocalAuthEnabled: false})
	require.NoError(t, err)
}

func TestUpdateAuthSettings_CannotDisableWithoutAdminLink(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: true,
	}, nil)
	opr.EXPECT().FilterEnabledProviders(ctx).Return([]*oauthprovider.Provider{
		{ID: 1, Canonical: "github", Name: "GitHub", Enabled: true},
	}, nil)
	// Admin user with NO linked OAuth account
	s.Users.EXPECT().Filter(ctx).Return([]*user.User{
		{ID: 1, Username: "admin", Admin: true},
	}, nil)
	opr.EXPECT().FindUserLinksByUser(ctx, uint32(1)).Return([]*oauthprovider.UserLink{}, nil)

	err := s.S.UpdateAuthSettings(ctx, oauthprovider.AuthSettings{LocalAuthEnabled: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no admin user has a linked OAuth account")
}

func TestUpdateAuthSettings_NonAdminLinkDoesNotCount(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: true,
	}, nil)
	opr.EXPECT().FilterEnabledProviders(ctx).Return([]*oauthprovider.Provider{
		{ID: 1, Canonical: "github", Name: "GitHub", Enabled: true},
	}, nil)
	// Non-admin has a link, admin does not
	s.Users.EXPECT().Filter(ctx).Return([]*user.User{
		{ID: 1, Username: "admin", Admin: true},
		{ID: 2, Username: "regular", Admin: false},
	}, nil)
	opr.EXPECT().FindUserLinksByUser(ctx, uint32(1)).Return([]*oauthprovider.UserLink{}, nil)

	err := s.S.UpdateAuthSettings(ctx, oauthprovider.AuthSettings{LocalAuthEnabled: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no admin user has a linked OAuth account")
}

func TestOAuthCompleteProfile_Valid(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	// Generate a valid temp token
	tempToken, err := pikoci.GenerateTempToken(
		[]byte("test-secret"),
		"github", "sub-123", "user@example.com", "testuser", "Test User",
	)
	require.NoError(t, err)

	opr.EXPECT().FindProviderByCanonical(ctx, "github").Return(&oauthprovider.Provider{
		ID:        1,
		Canonical: "github",
		Name:      "GitHub",
		Type:      "oauth2",
	}, nil)

	s.Users.EXPECT().Create(ctx, gomock.Any()).Return(uint32(10), nil)

	opr.EXPECT().CreateUserLink(ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, link oauthprovider.UserLink) (uint32, error) {
			assert.Equal(t, uint32(10), link.UserID)
			assert.Equal(t, uint32(1), link.ProviderID)
			assert.Equal(t, "sub-123", link.Subject)
			assert.Equal(t, "user@example.com", link.Email)
			return uint32(1), nil
		},
	)

	expectedUM := &user.WithMemberships{
		User: user.User{ID: 10, Username: "testuser"},
	}
	s.Users.EXPECT().FindWithMemberships(ctx, "testuser").Return(expectedUM, nil)

	um, jwtToken, err := s.S.OAuthCompleteProfile(ctx, tempToken, "testuser", "Test User")
	require.NoError(t, err)
	require.NotNil(t, um)
	assert.Equal(t, "testuser", um.Username)
	assert.NotEmpty(t, jwtToken)
}

func TestOAuthCompleteProfile_InvalidToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	_, _, err := s.S.OAuthCompleteProfile(ctx, "not-a-valid-jwt", "testuser", "Test User")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
}

func TestOAuthCompleteProfile_ExpiredToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	// Create a token that is already expired
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"purpose":  "profile_completion",
		"provider": "github",
		"subject":  "sub-123",
		"email":    "user@example.com",
		"exp":      time.Now().Add(-10 * time.Minute).Unix(),
	})
	expiredToken, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, _, err = s.S.OAuthCompleteProfile(ctx, expiredToken, "testuser", "Test User")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
}

func TestOAuthCompleteProfile_InvalidUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, _ := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	tempToken, err := pikoci.GenerateTempToken(
		[]byte("test-secret"),
		"github", "sub-123", "user@example.com", "testuser", "Test User",
	)
	require.NoError(t, err)

	_, _, err = s.S.OAuthCompleteProfile(ctx, tempToken, "INVALID USER NAME!", "Test User")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid username format")
}

func TestGenerateTempToken_RoundTrip(t *testing.T) {
	secret := []byte("test-secret")

	tokenStr, err := pikoci.GenerateTempToken(
		secret, "my-provider", "subject-abc", "test@example.com", "suggesteduser", "Full Name",
	)
	require.NoError(t, err)
	require.NotEmpty(t, tokenStr)

	// Parse the token back
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)

	assert.Equal(t, "profile_completion", claims["purpose"])
	assert.Equal(t, "my-provider", claims["provider"])
	assert.Equal(t, "subject-abc", claims["subject"])
	assert.Equal(t, "test@example.com", claims["email"])
	assert.Equal(t, "suggesteduser", claims["suggested_username"])
	assert.Equal(t, "Full Name", claims["full_name"])

	// Verify expiration is set and in the future
	exp, err := claims.GetExpirationTime()
	require.NoError(t, err)
	assert.True(t, exp.After(time.Now()))
}

func TestUserLogin_LocalAuthDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: false,
	}, nil)

	_, _, err := s.S.UserLogin(ctx, "testuser", "password123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local authentication is disabled")
}

func TestUserLogin_LocalAuthEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	plainPassword := "mysecretpassword"
	hashedPassword, err := utils.HashPassword(plainPassword)
	require.NoError(t, err)

	opr.EXPECT().GetAuthSettings(ctx).Return(&oauthprovider.AuthSettings{
		ID:               1,
		LocalAuthEnabled: true,
	}, nil)

	s.Users.EXPECT().FindWithMemberships(ctx, "testuser").Return(&user.WithMemberships{
		User: user.User{
			ID:       1,
			Username: "testuser",
			Password: hashedPassword,
		},
	}, nil)

	um, token, err := s.S.UserLogin(ctx, "testuser", plainPassword)
	require.NoError(t, err)
	require.NotNil(t, um)
	assert.Equal(t, "testuser", um.Username)
	assert.NotEmpty(t, token)
}

func TestUpdateOAuthProvider_MergeLogic(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	existing := &oauthprovider.Provider{
		ID:            1,
		Canonical:     "my-provider",
		Name:          "My Provider",
		Type:          "oidc",
		IssuerURL:     "https://old-issuer.example.com",
		AuthURL:       "https://old-auth.example.com",
		TokenURL:      "https://old-token.example.com",
		UserinfoURL:   "https://old-userinfo.example.com",
		Scopes:        "openid email",
		ClientID:      "old-client-id",
		ClientSecret:  "old-client-secret",
		UsernameClaim: "email",
		Enabled:       true,
	}

	opr.EXPECT().FindProviderByCanonical(ctx, "my-provider").Return(existing, nil)

	opr.EXPECT().UpdateProvider(ctx, "my-provider", gomock.Any()).DoAndReturn(
		func(_ context.Context, canonical string, updated oauthprovider.Provider) error {
			// Non-empty fields from update should be applied
			assert.Equal(t, "Updated Provider", updated.Name)
			assert.Equal(t, "custom-scopes", updated.Scopes)
			assert.Equal(t, "new-client-id", updated.ClientID)
			assert.Equal(t, "new-client-secret", updated.ClientSecret)
			assert.Equal(t, "preferred_username", updated.UsernameClaim)

			// URL fields are always overwritten (even to empty)
			assert.Equal(t, "https://new-issuer.example.com", updated.IssuerURL)
			assert.Equal(t, "", updated.AuthURL)
			assert.Equal(t, "", updated.TokenURL)
			assert.Equal(t, "", updated.UserinfoURL)

			// Enabled is always overwritten
			assert.Equal(t, false, updated.Enabled)

			// Type should remain from existing since update.Type is empty
			assert.Equal(t, "oidc", updated.Type)

			return nil
		},
	)

	update := oauthprovider.Provider{
		Name:          "Updated Provider",
		IssuerURL:     "https://new-issuer.example.com",
		Scopes:        "custom-scopes",
		ClientID:      "new-client-id",
		ClientSecret:  "new-client-secret",
		UsernameClaim: "preferred_username",
		Enabled:       false,
		// Type, AuthURL, TokenURL, UserinfoURL left empty
	}

	result, err := s.S.UpdateOAuthProvider(ctx, "my-provider", update)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Updated Provider", result.Name)
}

func TestListLinkedAccounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	userID := uint32(42)

	opr.EXPECT().FindUserLinksByUser(ctx, userID).Return([]*oauthprovider.UserLink{
		{ID: 1, UserID: userID, ProviderID: 10, Subject: "sub-aaa", Email: "alice@example.com"},
		{ID: 2, UserID: userID, ProviderID: 20, Subject: "sub-bbb", Email: "bob@example.com"},
	}, nil)

	opr.EXPECT().FindProvider(ctx, uint32(10)).Return(&oauthprovider.Provider{
		ID:        10,
		Canonical: "github",
		Name:      "GitHub",
	}, nil)
	opr.EXPECT().FindProvider(ctx, uint32(20)).Return(&oauthprovider.Provider{
		ID:        20,
		Canonical: "google",
		Name:      "Google",
	}, nil)

	accounts, err := s.S.ListLinkedAccounts(ctx, userID)
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	assert.Equal(t, "github", accounts[0].ProviderCanonical)
	assert.Equal(t, "GitHub", accounts[0].ProviderName)
	assert.Equal(t, "alice@example.com", accounts[0].Email)
	assert.Equal(t, "sub-aaa", accounts[0].Subject)

	assert.Equal(t, "google", accounts[1].ProviderCanonical)
	assert.Equal(t, "Google", accounts[1].ProviderName)
	assert.Equal(t, "bob@example.com", accounts[1].Email)
	assert.Equal(t, "sub-bbb", accounts[1].Subject)
}

func TestUnlinkAccount(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	userID := uint32(42)

	opr.EXPECT().FindProviderByCanonical(ctx, "github").Return(&oauthprovider.Provider{
		ID:        10,
		Canonical: "github",
		Name:      "GitHub",
	}, nil)

	opr.EXPECT().DeleteUserLinkByUserAndProvider(ctx, userID, uint32(10)).Return(nil)

	err := s.S.UnlinkAccount(ctx, userID, "github")
	require.NoError(t, err)
}

func TestOAuthCompleteProfile_DuplicateUsername(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	tempToken, err := pikoci.GenerateTempToken(
		[]byte("test-secret"),
		"github", "sub-123", "user@example.com", "testuser", "Test User",
	)
	require.NoError(t, err)

	opr.EXPECT().FindProviderByCanonical(ctx, "github").Return(&oauthprovider.Provider{
		ID:        1,
		Canonical: "github",
		Name:      "GitHub",
		Type:      "oauth2",
	}, nil)

	s.Users.EXPECT().Create(ctx, gomock.Any()).Return(uint32(0), fmt.Errorf("UNIQUE constraint failed: users.username"))

	_, _, err = s.S.OAuthCompleteProfile(ctx, tempToken, "testuser", "Test User")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create user")
	assert.Contains(t, err.Error(), "UNIQUE constraint failed")
}

func TestChangePassword_OAuthUserNoCurrentPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	hash, _ := utils.HashPassword("randompass")
	s.Users.EXPECT().Find(ctx, "oauthuser").Return(&user.User{
		ID: 5, Username: "oauthuser", Password: hash,
	}, nil)
	// User has OAuth links — skip old password check
	opr.EXPECT().FindUserLinksByUser(ctx, uint32(5)).Return([]*oauthprovider.UserLink{
		{ID: 1, UserID: 5, ProviderID: 1},
	}, nil)
	s.Users.EXPECT().Update(ctx, "oauthuser", gomock.Any()).Return(nil)

	err := s.S.ChangePassword(ctx, "oauthuser", "", "newpassword")
	require.NoError(t, err)
}

func TestChangePassword_LocalUserRequiresCurrentPassword(t *testing.T) {
	ctrl := gomock.NewController(t)
	s, opr := newServiceWithOAuth(ctrl)
	ctx := context.TODO()

	s.Users.EXPECT().Find(ctx, "localuser").Return(&user.User{
		ID: 6, Username: "localuser", Password: "somehash",
	}, nil)
	// User has no OAuth links — current password required
	opr.EXPECT().FindUserLinksByUser(ctx, uint32(6)).Return([]*oauthprovider.UserLink{}, nil)

	err := s.S.ChangePassword(ctx, "localuser", "", "newpassword")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "current password is required")
}
