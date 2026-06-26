package pikoci

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/role"
)

func (q *PikoCI) CreateApiToken(ctx context.Context, username, name string, personal bool, teamCanonical string, tokenRole role.Role, expiresAt *time.Time) (*apitoken.WithPlaintext, error) {
	if name == "" {
		return nil, fmt.Errorf("token name is required")
	}
	if len(name) > 255 {
		return nil, fmt.Errorf("token name must be 255 characters or fewer")
	}

	um, err := q.Users.Find(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	t := apitoken.Token{
		Name:     name,
		UserID:   um.ID,
		Username: username,
		Personal: personal,
	}

	if !personal {
		if teamCanonical == "" {
			return nil, fmt.Errorf("team canonical is required for team-scoped tokens")
		}
		if !tokenRole.Assignable() {
			return nil, fmt.Errorf("invalid role %q: must be one of viewer, operator, maintainer, admin", tokenRole)
		}
		uwm, err := q.GetUser(ctx, username)
		if err != nil {
			return nil, fmt.Errorf("failed to get user memberships: %w", err)
		}
		if !uwm.HasRole(tokenRole, teamCanonical) {
			return nil, fmt.Errorf("user does not have %s role on team %q", tokenRole, teamCanonical)
		}
		t.TeamCanonical = teamCanonical
		t.Role = tokenRole
	}

	if expiresAt != nil {
		if expiresAt.Before(time.Now()) {
			return nil, fmt.Errorf("expiration time must be in the future")
		}
		t.ExpiresAt = expiresAt
	}

	plaintext, hash, prefix, err := generateApiToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	t.TokenPrefix = prefix

	id, err := q.ApiTokens.Create(ctx, t, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}

	t.ID = id
	t.CreatedAt = time.Now().UTC()

	return &apitoken.WithPlaintext{
		Token:     t,
		Plaintext: plaintext,
	}, nil
}

func (q *PikoCI) ListApiTokens(ctx context.Context, username string) ([]*apitoken.Token, error) {
	tokens, err := q.ApiTokens.Filter(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to list API tokens: %w", err)
	}
	return tokens, nil
}

func (q *PikoCI) DeleteApiToken(ctx context.Context, username string, tokenID uint32) error {
	err := q.ApiTokens.Delete(ctx, username, tokenID)
	if err != nil {
		return fmt.Errorf("failed to delete API token: %w", err)
	}
	return nil
}

func (q *PikoCI) FindApiTokenByHash(ctx context.Context, tokenHash string) (*apitoken.AuthResult, error) {
	return q.ApiTokens.FindByHash(ctx, tokenHash)
}

func (q *PikoCI) UpdateApiTokenLastUsed(ctx context.Context, tokenID uint32) {
	if err := q.ApiTokens.UpdateLastUsed(ctx, tokenID); err != nil {
		q.logger.Error("failed to update API token last used", "token_id", tokenID, "error", err)
	}
}

func generateApiToken() (plaintext, hash, prefix string, err error) {
	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		return
	}
	hexStr := hex.EncodeToString(b)
	plaintext = "pko_" + hexStr
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	prefix = "pko_" + hexStr[:8]
	return
}
