package oauthprovider

import "time"

// Provider holds the full configuration for an OAuth/OIDC provider.
type Provider struct {
	ID            uint32    `json:"id"`
	Name          string    `json:"name"`
	Canonical     string    `json:"canonical"`
	Type          string    `json:"type"` // "oidc" or "oauth2"
	IssuerURL     string    `json:"issuer_url,omitempty"`
	AuthURL       string    `json:"auth_url,omitempty"`
	TokenURL      string    `json:"token_url,omitempty"`
	UserinfoURL   string    `json:"userinfo_url,omitempty"`
	Scopes        string    `json:"scopes"`
	ClientID      string    `json:"client_id"`
	ClientSecret  string    `json:"-"`
	UsernameClaim string    `json:"username_claim"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// PublicProvider is a Provider with secrets stripped, safe for the login page.
type PublicProvider struct {
	Canonical string `json:"canonical"`
	Name      string `json:"name"`
	Type      string `json:"type"`
}

// UserLink represents the association between a PikoCI user and an OAuth identity.
type UserLink struct {
	ID         uint32 `json:"id"`
	UserID     uint32 `json:"user_id"`
	ProviderID uint32 `json:"provider_id"`
	Subject    string `json:"subject"`
	Email      string `json:"email,omitempty"`
}

// AuthSettings holds global authentication settings.
type AuthSettings struct {
	ID               uint32 `json:"id"`
	LocalAuthEnabled bool   `json:"local_auth_enabled"`
}

// AuthMethods describes the available authentication methods for the login page.
type AuthMethods struct {
	LocalAuthEnabled bool             `json:"local_auth_enabled"`
	Providers        []PublicProvider `json:"providers"`
}

// LinkedAccount represents a user's linked OAuth provider for display.
type LinkedAccount struct {
	ProviderCanonical string `json:"provider_canonical"`
	ProviderName      string `json:"provider_name"`
	Email             string `json:"email,omitempty"`
	Subject           string `json:"subject"`
}
