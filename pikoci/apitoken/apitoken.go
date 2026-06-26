package apitoken

import (
	"time"

	"github.com/pikoci/pikoci/pikoci/role"
)

// Token represents an API token for authenticated access.
type Token struct {
	ID            uint32     `json:"id"`
	Name          string     `json:"name"`
	TokenPrefix   string     `json:"token_prefix"`
	Personal      bool       `json:"personal"`
	UserID        uint32     `json:"-"`
	Username      string     `json:"username"`
	TeamCanonical string     `json:"team_canonical,omitempty"`
	Role          role.Role  `json:"role,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}

// WithPlaintext wraps a Token with the plaintext value shown only at creation.
type WithPlaintext struct {
	Token
	Plaintext string `json:"token"`
}

// AuthResult contains the information needed to authorize a request
// authenticated via an API token.
type AuthResult struct {
	Username      string
	UserID        uint32
	UserAdmin     bool
	Personal      bool
	TeamCanonical string
	TokenRole     role.Role
	ExpiresAt     *time.Time
	TokenID       uint32
}
