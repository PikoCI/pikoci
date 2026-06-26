package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/apitoken"
	"github.com/pikoci/pikoci/pikoci/role"
)

type ApiTokenRepository struct {
	querier sqlr.Querier
}

func NewApiTokenRepository(db sqlr.Querier) *ApiTokenRepository {
	return &ApiTokenRepository{querier: db}
}

func (r *ApiTokenRepository) Create(ctx context.Context, t apitoken.Token, tokenHash string) (uint32, error) {
	var teamID sql.NullInt64
	var rl sql.NullString
	if !t.Personal {
		// Look up team_id from canonical
		err := r.querier.QueryRowContext(ctx, `SELECT id FROM teams WHERE canonical = ?`, t.TeamCanonical).Scan(&teamID)
		if err != nil {
			return 0, fmt.Errorf("failed to find team %q: %w", t.TeamCanonical, err)
		}
		rl = toNullString(string(t.Role))
	}

	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO api_tokens (name, token_hash, token_prefix, user_id, team_id, role, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, t.Name, tokenHash, t.TokenPrefix, t.UserID, teamID, rl, toNullTimePtr(t.ExpiresAt))
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("you already have a token named %q", t.Name)
		}
		return 0, fmt.Errorf("failed to insert api_token: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *ApiTokenRepository) FindByHash(ctx context.Context, tokenHash string) (*apitoken.AuthResult, error) {
	var (
		ar            apitoken.AuthResult
		teamCanonical sql.NullString
		tokenRole     sql.NullString
		expiresAt     sql.NullTime
	)

	err := r.querier.QueryRowContext(ctx, `
		SELECT
			at.id,
			u.username,
			u.id,
			u.admin,
			t.canonical,
			at.role,
			at.expires_at
		FROM api_tokens at
		JOIN users u ON u.id = at.user_id
		LEFT JOIN teams t ON t.id = at.team_id
		WHERE at.token_hash = ?
	`, tokenHash).Scan(
		&ar.TokenID,
		&ar.Username,
		&ar.UserID,
		&ar.UserAdmin,
		&teamCanonical,
		&tokenRole,
		&expiresAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid API token")
		}
		return nil, fmt.Errorf("failed to find api_token: %w", err)
	}

	ar.TeamCanonical = teamCanonical.String
	ar.TokenRole = role.Role(tokenRole.String)
	if expiresAt.Valid {
		ar.ExpiresAt = &expiresAt.Time
	}
	ar.Personal = !teamCanonical.Valid

	return &ar, nil
}

func (r *ApiTokenRepository) Filter(ctx context.Context, username string) ([]*apitoken.Token, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT
			at.id,
			at.name,
			at.token_prefix,
			u.username,
			t.canonical,
			at.role,
			at.expires_at,
			at.created_at,
			at.last_used_at
		FROM api_tokens at
		JOIN users u ON u.id = at.user_id
		LEFT JOIN teams t ON t.id = at.team_id
		WHERE u.username = ?
		ORDER BY at.created_at DESC
	`, username)
	if err != nil {
		return nil, fmt.Errorf("failed to query api_tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*apitoken.Token
	for rows.Next() {
		var (
			t             apitoken.Token
			teamCanonical sql.NullString
			tokenRole     sql.NullString
			expiresAt     sql.NullTime
			lastUsedAt    sql.NullTime
		)
		err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.TokenPrefix,
			&t.Username,
			&teamCanonical,
			&tokenRole,
			&expiresAt,
			&t.CreatedAt,
			&lastUsedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan api_token row: %w", err)
		}
		t.TeamCanonical = teamCanonical.String
		t.Role = role.Role(tokenRole.String)
		if expiresAt.Valid {
			t.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			t.LastUsedAt = &lastUsedAt.Time
		}
		t.Personal = !teamCanonical.Valid
		tokens = append(tokens, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return tokens, nil
}

func (r *ApiTokenRepository) Delete(ctx context.Context, username string, tokenID uint32) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE FROM api_tokens
		WHERE id = ? AND user_id = (SELECT id FROM users WHERE username = ?)
	`, tokenID, username)
	if err != nil {
		return fmt.Errorf("failed to delete api_token: %w", err)
	}
	return isEntityFound(res)
}

func (r *ApiTokenRepository) DeleteByTeamMember(ctx context.Context, username, teamCanonical string) error {
	_, err := r.querier.ExecContext(ctx, `
		DELETE FROM api_tokens
		WHERE user_id = (SELECT id FROM users WHERE username = ?)
		  AND team_id = (SELECT id FROM teams WHERE canonical = ?)
	`, username, teamCanonical)
	if err != nil {
		return fmt.Errorf("failed to delete api_tokens for team member: %w", err)
	}
	return nil
}

func (r *ApiTokenRepository) UpdateLastUsed(ctx context.Context, tokenID uint32) error {
	_, err := r.querier.ExecContext(ctx, `
		UPDATE api_tokens SET last_used_at = ? WHERE id = ?
	`, time.Now().UTC(), tokenID)
	if err != nil {
		return fmt.Errorf("failed to update last_used_at: %w", err)
	}
	return nil
}
