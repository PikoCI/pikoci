package pikoci

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gosimple/slug"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
	"golang.org/x/oauth2"
)


// OAuthState stores the CSRF state for an OAuth flow.
type OAuthState struct {
	Nonce     string
	UserID    uint32 // non-zero for account linking
	Link      bool
	CreatedAt time.Time
}

// OAuthStateStore manages in-memory OAuth CSRF state with expiration.
type OAuthStateStore struct {
	mu     sync.Mutex
	states map[string]*OAuthState
}

// NewOAuthStateStore creates a new state store and starts a purge goroutine.
func NewOAuthStateStore(ctx context.Context) *OAuthStateStore {
	s := &OAuthStateStore{
		states: make(map[string]*OAuthState),
	}
	go s.purgeLoop(ctx)
	return s
}

func (s *OAuthStateStore) Set(state string, os *OAuthState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state] = os
}

func (s *OAuthStateStore) Get(state string) (*OAuthState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	os, ok := s.states[state]
	if !ok {
		return nil, false
	}
	delete(s.states, state)
	if time.Since(os.CreatedAt) > 5*time.Minute {
		return nil, false
	}
	return os, true
}

func (s *OAuthStateStore) purgeLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			for k, v := range s.states {
				if time.Since(v.CreatedAt) > 5*time.Minute {
					delete(s.states, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

// GenerateState generates a random hex string for OAuth CSRF state.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GetAuthMethods returns enabled auth methods for the login page.
func (q *PikoCI) GetAuthMethods(ctx context.Context) (*oauthprovider.AuthMethods, error) {
	settings, err := q.OAuthProviders.GetAuthSettings(ctx)
	if err != nil {
		return &oauthprovider.AuthMethods{LocalAuthEnabled: true}, nil
	}

	providers, err := q.OAuthProviders.FilterEnabledProviders(ctx)
	if err != nil {
		return &oauthprovider.AuthMethods{LocalAuthEnabled: settings.LocalAuthEnabled}, nil
	}

	publicProviders := make([]oauthprovider.PublicProvider, 0, len(providers))
	for _, p := range providers {
		publicProviders = append(publicProviders, oauthprovider.PublicProvider{
			Canonical: p.Canonical,
			Name:      p.Name,
			Type:      p.Type,
		})
	}

	return &oauthprovider.AuthMethods{
		LocalAuthEnabled: settings.LocalAuthEnabled,
		Providers:        publicProviders,
	}, nil
}

// GetOAuthProvider retrieves a single OAuth provider by canonical.
func (q *PikoCI) GetOAuthProvider(ctx context.Context, canonical string) (*oauthprovider.Provider, error) {
	p, err := q.OAuthProviders.FindProviderByCanonical(ctx, canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to find OAuth provider: %w", err)
	}
	return p, nil
}

// CreateOAuthProvider creates a new OAuth provider configuration.
func (q *PikoCI) CreateOAuthProvider(ctx context.Context, p oauthprovider.Provider) (*oauthprovider.Provider, error) {
	if p.Name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	if p.Canonical == "" {
		p.Canonical = slug.Make(p.Name)
	}
	if p.Type != "oidc" && p.Type != "oauth2" {
		return nil, fmt.Errorf("provider type must be 'oidc' or 'oauth2'")
	}
	if p.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if p.ClientSecret == "" {
		return nil, fmt.Errorf("client_secret is required")
	}
	if p.Type == "oidc" && p.IssuerURL == "" {
		return nil, fmt.Errorf("issuer_url is required for OIDC providers")
	}
	if p.Type == "oauth2" {
		if p.AuthURL == "" || p.TokenURL == "" {
			return nil, fmt.Errorf("auth_url and token_url are required for OAuth2 providers")
		}
	}
	if p.Scopes == "" {
		if p.Type == "oidc" {
			p.Scopes = "openid email profile"
		} else {
			p.Scopes = "user:email"
		}
	}
	if p.UsernameClaim == "" {
		p.UsernameClaim = "email"
	}

	id, err := q.OAuthProviders.CreateProvider(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("failed to create OAuth provider: %w", err)
	}

	p.ID = id
	return &p, nil
}

// ListOAuthProviders returns all OAuth provider configurations.
func (q *PikoCI) ListOAuthProviders(ctx context.Context) ([]*oauthprovider.Provider, error) {
	ps, err := q.OAuthProviders.FilterProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list OAuth providers: %w", err)
	}
	return ps, nil
}

// UpdateOAuthProvider updates an existing OAuth provider.
func (q *PikoCI) UpdateOAuthProvider(ctx context.Context, canonical string, p oauthprovider.Provider) (*oauthprovider.Provider, error) {
	existing, err := q.OAuthProviders.FindProviderByCanonical(ctx, canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to find OAuth provider: %w", err)
	}

	if p.Name != "" {
		existing.Name = p.Name
	}
	if p.Canonical != "" {
		existing.Canonical = p.Canonical
	}
	if p.Type != "" {
		existing.Type = p.Type
	}
	existing.IssuerURL = p.IssuerURL
	existing.AuthURL = p.AuthURL
	existing.TokenURL = p.TokenURL
	existing.UserinfoURL = p.UserinfoURL
	if p.Scopes != "" {
		existing.Scopes = p.Scopes
	}
	if p.ClientID != "" {
		existing.ClientID = p.ClientID
	}
	if p.ClientSecret != "" {
		existing.ClientSecret = p.ClientSecret
	}
	if p.UsernameClaim != "" {
		existing.UsernameClaim = p.UsernameClaim
	}
	existing.Enabled = p.Enabled

	// Re-validate after merge
	if existing.Type == "oidc" && existing.IssuerURL == "" {
		return nil, fmt.Errorf("issuer_url is required for OIDC providers")
	}
	if existing.Type == "oauth2" && (existing.AuthURL == "" || existing.TokenURL == "") {
		return nil, fmt.Errorf("auth_url and token_url are required for OAuth2 providers")
	}

	err = q.OAuthProviders.UpdateProvider(ctx, canonical, *existing)
	if err != nil {
		return nil, fmt.Errorf("failed to update OAuth provider: %w", err)
	}

	return existing, nil
}

// DeleteOAuthProvider deletes an OAuth provider by canonical.
// It prevents deletion if any user relies on this provider as their only
// authentication method (no local password and no other OAuth links).
func (q *PikoCI) DeleteOAuthProvider(ctx context.Context, canonical string) error {
	provider, err := q.OAuthProviders.FindProviderByCanonical(ctx, canonical)
	if err != nil {
		return fmt.Errorf("failed to find OAuth provider: %w", err)
	}

	// Check if any user would be locked out
	users, err := q.Users.Filter(ctx)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}
	for _, u := range users {
		if u.Password != "" {
			continue // has local password, won't be locked out
		}
		links, err := q.OAuthProviders.FindUserLinksByUser(ctx, u.ID)
		if err != nil || len(links) == 0 {
			continue
		}
		// Check if this provider is their only link
		if len(links) == 1 && links[0].ProviderID == provider.ID {
			return fmt.Errorf("cannot delete provider %q: user %q has no local password and no other OAuth links", canonical, u.Username)
		}
	}

	err = q.OAuthProviders.DeleteProvider(ctx, canonical)
	if err != nil {
		return fmt.Errorf("failed to delete OAuth provider: %w", err)
	}
	return nil
}

// GetAuthSettings returns the global auth settings.
func (q *PikoCI) GetAuthSettings(ctx context.Context) (*oauthprovider.AuthSettings, error) {
	settings, err := q.OAuthProviders.GetAuthSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get auth settings: %w", err)
	}
	return settings, nil
}

// UpdateAuthSettings updates the global auth settings.
func (q *PikoCI) UpdateAuthSettings(ctx context.Context, settings oauthprovider.AuthSettings) error {
	existing, err := q.OAuthProviders.GetAuthSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get auth settings: %w", err)
	}
	settings.ID = existing.ID

	// Safety: cannot disable local auth unless an admin user has a linked OAuth account
	if !settings.LocalAuthEnabled {
		providers, err := q.OAuthProviders.FilterEnabledProviders(ctx)
		if err != nil || len(providers) == 0 {
			return fmt.Errorf("cannot disable local auth: no enabled OAuth providers configured")
		}

		// Check that at least one admin has a linked OAuth account
		admins, err := q.Users.Filter(ctx)
		if err != nil {
			return fmt.Errorf("failed to list users: %w", err)
		}
		adminHasLink := false
		for _, u := range admins {
			if !u.Admin {
				continue
			}
			links, err := q.OAuthProviders.FindUserLinksByUser(ctx, u.ID)
			if err == nil && len(links) > 0 {
				adminHasLink = true
				break
			}
		}
		if !adminHasLink {
			return fmt.Errorf("cannot disable local auth: no admin user has a linked OAuth account")
		}
	}

	err = q.OAuthProviders.UpdateAuthSettings(ctx, settings)
	if err != nil {
		return fmt.Errorf("failed to update auth settings: %w", err)
	}
	return nil
}

// ListLinkedAccounts returns the OAuth links for a user.
func (q *PikoCI) ListLinkedAccounts(ctx context.Context, userID uint32) ([]*oauthprovider.LinkedAccount, error) {
	links, err := q.OAuthProviders.FindUserLinksByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user links: %w", err)
	}

	accounts := make([]*oauthprovider.LinkedAccount, 0, len(links))
	for _, l := range links {
		provider, err := q.OAuthProviders.FindProvider(ctx, l.ProviderID)
		if err != nil {
			continue
		}
		accounts = append(accounts, &oauthprovider.LinkedAccount{
			ProviderCanonical: provider.Canonical,
			ProviderName:      provider.Name,
			Email:             l.Email,
			Subject:           l.Subject,
		})
	}
	return accounts, nil
}

// UnlinkAccount removes an OAuth link for a user. It prevents unlinking
// if it would leave the user with no way to log in (no local password
// and no other OAuth links).
func (q *PikoCI) UnlinkAccount(ctx context.Context, userID uint32, canonical string) error {
	provider, err := q.OAuthProviders.FindProviderByCanonical(ctx, canonical)
	if err != nil {
		return fmt.Errorf("failed to find provider: %w", err)
	}

	// Check if unlinking would lock the user out
	u, err := q.Users.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}
	if u.Password == "" {
		// No local password — check if they have other OAuth links
		links, err := q.OAuthProviders.FindUserLinksByUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to check user links: %w", err)
		}
		otherLinks := 0
		for _, l := range links {
			if l.ProviderID != provider.ID {
				otherLinks++
			}
		}
		if otherLinks == 0 {
			return fmt.Errorf("cannot unlink: this is your only authentication method. Set a local password first or link another provider")
		}
	}

	err = q.OAuthProviders.DeleteUserLinkByUserAndProvider(ctx, userID, provider.ID)
	if err != nil {
		return fmt.Errorf("failed to unlink account: %w", err)
	}
	return nil
}

// FindOAuthUserLink looks up an OAuth user link by provider ID and subject.
func (q *PikoCI) FindOAuthUserLink(ctx context.Context, providerID uint32, subject string) (*oauthprovider.UserLink, error) {
	return q.OAuthProviders.FindUserLink(ctx, providerID, subject)
}

// CreateOAuthUserLink creates a new OAuth user link.
func (q *PikoCI) CreateOAuthUserLink(ctx context.Context, link oauthprovider.UserLink) (uint32, error) {
	return q.OAuthProviders.CreateUserLink(ctx, link)
}

// FindUserByID retrieves a user by their ID.
func (q *PikoCI) FindUserByID(ctx context.Context, userID uint32) (*user.User, error) {
	return q.Users.FindByID(ctx, userID)
}

// OAuthFlowConfig holds the oauth2.Config and, for OIDC providers, the discovered provider.
type OAuthFlowConfig struct {
	OAuth2     *oauth2.Config
	OIDCProvider *oidc.Provider // non-nil only for OIDC type
}

// BuildOAuth2Config builds the oauth2.Config for a provider. For OIDC providers,
// it performs discovery once and stores the provider for later use in FetchOAuthUserInfo.
func BuildOAuth2Config(ctx context.Context, p *oauthprovider.Provider, callbackURL string) (*OAuthFlowConfig, error) {
	scopes := strings.Fields(p.Scopes)
	if p.Type == "oidc" {
		provider, err := oidc.NewProvider(ctx, p.IssuerURL)
		if err != nil {
			return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
		}
		return &OAuthFlowConfig{
			OAuth2: &oauth2.Config{
				ClientID:     p.ClientID,
				ClientSecret: p.ClientSecret,
				RedirectURL:  callbackURL,
				Endpoint:     provider.Endpoint(),
				Scopes:       scopes,
			},
			OIDCProvider: provider,
		}, nil
	}
	// Generic OAuth2
	return &OAuthFlowConfig{
		OAuth2: &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  callbackURL,
			Endpoint: oauth2.Endpoint{
				AuthURL:  p.AuthURL,
				TokenURL: p.TokenURL,
			},
			Scopes: scopes,
		},
	}, nil
}

// FetchOAuthUserInfo fetches user info from the provider after code exchange.
func FetchOAuthUserInfo(ctx context.Context, p *oauthprovider.Provider, token *oauth2.Token, flowCfg *OAuthFlowConfig) (subject, email, fullName, username string, err error) {
	if p.Type == "oidc" {
		verifier := flowCfg.OIDCProvider.Verifier(&oidc.Config{ClientID: p.ClientID})
		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			return "", "", "", "", fmt.Errorf("no id_token in token response")
		}
		idToken, err := verifier.Verify(ctx, rawIDToken)
		if err != nil {
			return "", "", "", "", fmt.Errorf("failed to verify id_token: %w", err)
		}
		var claims struct {
			Sub               string `json:"sub"`
			Email             string `json:"email"`
			Name              string `json:"name"`
			PreferredUsername string `json:"preferred_username"`
		}
		if err := idToken.Claims(&claims); err != nil {
			return "", "", "", "", fmt.Errorf("failed to parse id_token claims: %w", err)
		}
		subject = claims.Sub
		email = claims.Email
		fullName = claims.Name
		username = claims.PreferredUsername
		if username == "" && email != "" {
			username = strings.SplitN(email, "@", 2)[0]
		}
		return subject, email, fullName, username, nil
	}

	// Generic OAuth2: fetch userinfo endpoint
	if p.UserinfoURL == "" {
		return "", "", "", "", fmt.Errorf("userinfo_url required for OAuth2 providers")
	}

	client := flowCfg.OAuth2.Client(ctx, token)
	resp, err := client.Get(p.UserinfoURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to read userinfo body: %w", err)
	}

	var info map[string]interface{}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse userinfo: %w", err)
	}

	// Extract subject (id or sub)
	if v, ok := info["sub"].(string); ok {
		subject = v
	} else if v, ok := info["id"].(float64); ok {
		subject = fmt.Sprintf("%.0f", v)
	} else if v, ok := info["id"].(string); ok {
		subject = v
	}

	if v, ok := info["email"].(string); ok {
		email = v
	}
	if v, ok := info["name"].(string); ok {
		fullName = v
	}
	if v, ok := info["login"].(string); ok {
		username = v
	} else if v, ok := info["preferred_username"].(string); ok {
		username = v
	}
	if username == "" && email != "" {
		username = strings.SplitN(email, "@", 2)[0]
	}

	return subject, email, fullName, username, nil
}

// OAuthCompleteProfile creates a user from a temp OAuth token and links the account.
func (q *PikoCI) OAuthCompleteProfile(ctx context.Context, tempToken, username, fullName string) (*user.WithMemberships, string, error) {
	// Parse temp token
	token, err := jwt.Parse(tempToken, func(token *jwt.Token) (any, error) {
		return q.JWTSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, "", fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, "", fmt.Errorf("invalid token claims")
	}

	purpose, _ := claims["purpose"].(string)
	if purpose != "profile_completion" {
		return nil, "", fmt.Errorf("invalid token purpose")
	}

	providerCanonical, _ := claims["provider"].(string)
	subject, _ := claims["subject"].(string)
	emailClaim, _ := claims["email"].(string)

	if providerCanonical == "" || subject == "" {
		return nil, "", fmt.Errorf("invalid token: missing provider or subject")
	}

	if !utils.ValidateCanonical(username) {
		return nil, "", fmt.Errorf("invalid username format %q", username)
	}

	provider, err := q.OAuthProviders.FindProviderByCanonical(ctx, providerCanonical)
	if err != nil {
		return nil, "", fmt.Errorf("provider not found: %w", err)
	}

	// Create user with empty password sentinel (OAuth-only users).
	// Empty string is not a valid bcrypt hash, so local login is impossible
	// until the user explicitly sets a password via ChangePassword.
	u := user.User{
		Username: username,
		FullName: fullName,
		Password: "",
	}

	id, err := q.Users.Create(ctx, u)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create user: %w", err)
	}
	u.ID = id

	// Create OAuth link
	_, err = q.OAuthProviders.CreateUserLink(ctx, oauthprovider.UserLink{
		UserID:     u.ID,
		ProviderID: provider.ID,
		Subject:    subject,
		Email:      emailClaim,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create user link: %w", err)
	}

	um, err := q.Users.FindWithMemberships(ctx, u.Username)
	if err != nil {
		return nil, "", fmt.Errorf("failed to find user: %w", err)
	}
	um.HasPassword = um.Password != ""

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": um,
	})
	tokenString, err := jwtToken.SignedString(q.JWTSecret)
	if err != nil {
		return nil, "", fmt.Errorf("failed to sign token: %w", err)
	}

	return um, tokenString, nil
}

// GenerateTempToken creates a short-lived JWT for profile completion.
func GenerateTempToken(jwtSecret []byte, providerCanonical, subject, email, suggestedUsername, fullName string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"purpose":            "profile_completion",
		"provider":           providerCanonical,
		"subject":            subject,
		"email":              email,
		"suggested_username": suggestedUsername,
		"full_name":          fullName,
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
	})
	return token.SignedString(jwtSecret)
}
