package mysql

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/secret"
)

// serverKeyName is the fixed name of the single server identity row. The name
// column exists so a future rotation can hold more than one.
const serverKeyName = "default"

type SecretRepository struct {
	querier sqlr.Querier
}

func NewSecretRepository(db sqlr.Querier) *SecretRepository {
	return &SecretRepository{querier: db}
}

type dbSecret struct {
	ID        sql.NullInt64
	Name      sql.NullString
	Canonical sql.NullString
	Kind      sql.NullString
	Value     sql.NullString
	CreatedAt sql.NullTime
	UpdatedAt sql.NullTime
}

func (dbs *dbSecret) toDomainEntity(scope secret.Scope) *secret.Entry {
	e := &secret.Entry{
		ID:        uint32(dbs.ID.Int64),
		Name:      dbs.Name.String,
		Canonical: dbs.Canonical.String,
		Scope:     scope,
		Kind:      secret.Kind(dbs.Kind.String),
		CreatedAt: dbs.CreatedAt.Time,
		UpdatedAt: dbs.UpdatedAt.Time,
	}

	// Only plain values are ever surfaced. Secret ciphertext stays in the
	// database; nothing above this layer has a use for it.
	if e.Kind == secret.KindPlain {
		e.Value = dbs.Value.String
	}

	return e
}

// encodeValue renders a value for a TEXT column.
//
// Secret ciphertext is base64-encoded because it is binary and BLOB/BYTEA
// spelling differs across the supported backends, with no adaptSQL rule to
// translate it. Plain values are stored verbatim so an operator inspecting the
// database sees readable configuration, which is the point of the kind.
func encodeValue(kind secret.Kind, value []byte) string {
	if kind == secret.KindPlain {
		return string(value)
	}
	return base64.StdEncoding.EncodeToString(value)
}

func decodeValue(kind secret.Kind, s string) ([]byte, error) {
	if kind == secret.KindPlain {
		return []byte(s), nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// UpsertTeam stores an encrypted team-scoped secret.
//
// This is an UPDATE-then-INSERT rather than a native upsert because MySQL
// spells it ON DUPLICATE KEY UPDATE while SQLite and PostgreSQL use
// ON CONFLICT. Callers run it inside a transaction, and the unique constraint
// still backstops a concurrent insert.
func (r *SecretRepository) UpsertTeam(ctx context.Context, tc string, e secret.Entry, value []byte) (uint32, error) {
	teamID, err := r.resolveTeamID(ctx, tc)
	if err != nil {
		return 0, err
	}

	res, err := r.querier.ExecContext(ctx, `
		UPDATE team_secrets
		SET name = ?, value = ?, kind = ?, updated_at = CURRENT_TIMESTAMP
		WHERE canonical = ? AND team_id = ?
	`, e.Name, encodeValue(e.Kind, value), string(e.Kind), e.Canonical, teamID)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return r.findTeamID(ctx, tc, e.Canonical)
	}

	res, err = r.querier.ExecContext(ctx, `
		INSERT INTO team_secrets(name, canonical, value, kind, team_id)
		VALUES (?, ?, ?, ?, ?)
	`, e.Name, e.Canonical, encodeValue(e.Kind, value), string(e.Kind), teamID)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

// UpsertPipeline stores an encrypted pipeline-scoped secret.
func (r *SecretRepository) UpsertPipeline(ctx context.Context, tc, pn string, e secret.Entry, value []byte) (uint32, error) {
	pipelineID, err := r.resolvePipelineID(ctx, tc, pn)
	if err != nil {
		return 0, err
	}

	res, err := r.querier.ExecContext(ctx, `
		UPDATE pipeline_secrets
		SET name = ?, value = ?, kind = ?, updated_at = CURRENT_TIMESTAMP
		WHERE canonical = ? AND pipeline_id = ?
	`, e.Name, encodeValue(e.Kind, value), string(e.Kind), e.Canonical, pipelineID)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return r.findPipelineID(ctx, tc, pn, e.Canonical)
	}

	res, err = r.querier.ExecContext(ctx, `
		INSERT INTO pipeline_secrets(name, canonical, value, kind, pipeline_id)
		VALUES (?, ?, ?, ?, ?)
	`, e.Name, e.Canonical, encodeValue(e.Kind, value), string(e.Kind), pipelineID)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

// resolveTeamID reports a missing team plainly. Relying on a NULL subquery
// instead would surface as an opaque NOT NULL constraint violation.
func (r *SecretRepository) resolveTeamID(ctx context.Context, tc string) (uint32, error) {
	var id uint32
	err := r.querier.QueryRowContext(ctx, `SELECT id FROM teams WHERE canonical = ?`, tc).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("team %q not found", tc)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to find team %q: %w", tc, err)
	}
	return id, nil
}

func (r *SecretRepository) resolvePipelineID(ctx context.Context, tc, pn string) (uint32, error) {
	var id uint32
	err := r.querier.QueryRowContext(ctx, `
		SELECT p.id FROM pipelines AS p
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("pipeline %q not found in team %q", pn, tc)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to find pipeline %q: %w", pn, err)
	}
	return id, nil
}

func (r *SecretRepository) findTeamID(ctx context.Context, tc, sCan string) (uint32, error) {
	var id uint32
	err := r.querier.QueryRowContext(ctx, `
		SELECT s.id FROM team_secrets AS s
		JOIN teams AS t ON s.team_id = t.id
		WHERE t.canonical = ? AND s.canonical = ?
	`, tc, sCan).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to find secret: %w", err)
	}
	return id, nil
}

func (r *SecretRepository) findPipelineID(ctx context.Context, tc, pn, sCan string) (uint32, error) {
	var id uint32
	err := r.querier.QueryRowContext(ctx, `
		SELECT s.id FROM pipeline_secrets AS s
		JOIN pipelines AS p ON s.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND s.canonical = ?
	`, tc, pn, sCan).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to find secret: %w", err)
	}
	return id, nil
}

// FilterTeam returns the team-scoped secrets without their values.
func (r *SecretRepository) FilterTeam(ctx context.Context, tc string) ([]*secret.Entry, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT s.id, s.name, s.canonical, s.kind, s.value, s.created_at, s.updated_at
		FROM team_secrets AS s
		JOIN teams AS t ON s.team_id = t.id
		WHERE t.canonical = ?
		ORDER BY s.canonical
	`, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows, secret.TeamScope)
}

// FilterPipeline returns the pipeline-scoped secrets without their values.
func (r *SecretRepository) FilterPipeline(ctx context.Context, tc, pn string) ([]*secret.Entry, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT s.id, s.name, s.canonical, s.kind, s.value, s.created_at, s.updated_at
		FROM pipeline_secrets AS s
		JOIN pipelines AS p ON s.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
		ORDER BY s.canonical
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows, secret.PipelineScope)
}

func scanEntries(rows *sql.Rows, scope secret.Scope) ([]*secret.Entry, error) {
	entries := make([]*secret.Entry, 0)
	for rows.Next() {
		var dbs dbSecret
		if err := rows.Scan(&dbs.ID, &dbs.Name, &dbs.Canonical, &dbs.Kind, &dbs.Value, &dbs.CreatedAt, &dbs.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan secret: %w", err)
		}
		entries = append(entries, dbs.toDomainEntity(scope))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate entries: %w", err)
	}
	return entries, nil
}

// DeleteTeam removes a team-scoped secret.
func (r *SecretRepository) DeleteTeam(ctx context.Context, tc, sCan string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE FROM team_secrets
		WHERE canonical = ? AND team_id = (SELECT t.id FROM teams AS t WHERE t.canonical = ?)
	`, sCan, tc)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if err := isEntityFound(res); err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	return nil
}

// DeletePipeline removes a pipeline-scoped secret.
func (r *SecretRepository) DeletePipeline(ctx context.Context, tc, pn, sCan string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE FROM pipeline_secrets
		WHERE canonical = ? AND pipeline_id = (
			SELECT p.id FROM pipelines AS p
			JOIN teams AS t ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ?
		)
	`, sCan, tc, pn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if err := isEntityFound(res); err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	return nil
}

// StoredValues returns every value visible to a pipeline, keyed by canonical
// name, with pipeline-scoped entries shadowing team-scoped ones.
func (r *SecretRepository) StoredValues(ctx context.Context, tc, pn string) (map[string]secret.StoredValue, error) {
	values := make(map[string]secret.StoredValue)

	// Team scope first so the pipeline pass overwrites it.
	if err := r.collectValues(ctx, values, `
		SELECT s.canonical, s.value, s.kind
		FROM team_secrets AS s
		JOIN teams AS t ON s.team_id = t.id
		WHERE t.canonical = ?
	`, tc); err != nil {
		return nil, err
	}

	if err := r.collectValues(ctx, values, `
		SELECT s.canonical, s.value, s.kind
		FROM pipeline_secrets AS s
		JOIN pipelines AS p ON s.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn); err != nil {
		return nil, err
	}

	return values, nil
}

func (r *SecretRepository) collectValues(ctx context.Context, into map[string]secret.StoredValue, query string, args ...any) error {
	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var canonical, encoded, kind string
		if err := rows.Scan(&canonical, &encoded, &kind); err != nil {
			return fmt.Errorf("failed to scan stored value: %w", err)
		}
		value, err := decodeValue(secret.Kind(kind), encoded)
		if err != nil {
			return fmt.Errorf("failed to decode stored entry %q: %w", canonical, err)
		}
		into[canonical] = secret.StoredValue{Kind: secret.Kind(kind), Data: value}
	}

	return rows.Err()
}

// FindServerKey returns the stored wrapped identity, or nil when none exists.
func (r *SecretRepository) FindServerKey(ctx context.Context) (*secret.ServerKey, error) {
	var wrapped, recipient string
	err := r.querier.QueryRowContext(ctx, `
		SELECT wrapped, recipient FROM server_keys WHERE name = ?
	`, serverKeyName).Scan(&wrapped, &recipient)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("failed to decode the stored server key: %w", err)
	}

	return &secret.ServerKey{Wrapped: decoded, Recipient: recipient}, nil
}

// CreateServerKey persists a newly generated wrapped identity. The unique
// constraint on name makes a concurrent second insert fail rather than
// replacing an identity that stored values already depend on.
func (r *SecretRepository) CreateServerKey(ctx context.Context, k secret.ServerKey) error {
	_, err := r.querier.ExecContext(ctx, `
		INSERT INTO server_keys(name, wrapped, recipient) VALUES (?, ?, ?)
	`, serverKeyName, base64.StdEncoding.EncodeToString(k.Wrapped), k.Recipient)
	if err != nil {
		return fmt.Errorf("failed to store the server key: %w", err)
	}

	return nil
}
