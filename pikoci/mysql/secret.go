package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/secret"
)

type SecretRepository struct {
	querier sqlr.Querier
}

func NewSecretRepository(db sqlr.Querier) *SecretRepository {
	return &SecretRepository{
		querier: db,
	}
}

type dbSecret struct {
	ID        sql.NullInt64
	Name      sql.NullString
	Type      sql.NullString
	Canonical sql.NullString
	Params    sql.NullString
}

func newDBSecret(s secret.Secret) dbSecret {
	p, _ := json.Marshal(s.Params)
	return dbSecret{
		Name:      toNullString(s.Name),
		Type:      toNullString(s.Type),
		Canonical: toNullString(s.Canonical),
		Params:    toNullString(string(p)),
	}
}

func (dbs *dbSecret) toDomainEntity() *secret.Secret {
	s := &secret.Secret{
		ID:        uint32(dbs.ID.Int64),
		Name:      dbs.Name.String,
		Type:      dbs.Type.String,
		Canonical: dbs.Canonical.String,
	}

	_ = json.Unmarshal([]byte(dbs.Params.String), &s.Params)

	return s
}

func (r *SecretRepository) Create(ctx context.Context, tc, pn string, s secret.Secret) (uint32, error) {
	dbs := newDBSecret(s)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO secrets(name, `+"`type`"+`, canonical, params, pipeline_id)
		VALUES (?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.name = ?
			))`, dbs.Name, dbs.Type, dbs.Canonical, dbs.Params, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *SecretRepository) Update(ctx context.Context, tc, pn, sCan string, s secret.Secret) error {
	dbs := newDBSecret(s)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE secrets
		SET name = ?, `+"`type`"+` = ?, canonical = ?, params = ?
		WHERE id = (
			SELECT s.id
			FROM (SELECT * FROM secrets) AS s
			JOIN pipelines AS p
				ON s.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.name = ? AND s.canonical = ?
		)
	`, dbs.Name, dbs.Type, dbs.Canonical, dbs.Params, tc, pn, sCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}

	return nil
}

func (r *SecretRepository) Find(ctx context.Context, tc, pn, sCan string) (*secret.Secret, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT s.id, s.name, s.type, s.canonical, s.params
		FROM secrets AS s
		JOIN pipelines AS p
			ON s.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.name = ? AND s.canonical = ?
	`, tc, pn, sCan)

	s, err := scanSecret(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Secret: %w", err)
	}

	return s, nil
}

func (r *SecretRepository) Filter(ctx context.Context, tc, pn string) ([]*secret.Secret, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT s.id, s.name, s.type, s.canonical, s.params
		FROM secrets AS s
		JOIN pipelines AS p
			ON s.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.name = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Secrets: %w", err)
	}

	secrets, err := scanSecrets(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter secrets: %w", err)
	}

	return secrets, nil
}

func (r *SecretRepository) Delete(ctx context.Context, tc, pn, sCan string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM secrets
		WHERE id IN (
			SELECT s.id
			FROM secrets AS s
			JOIN pipelines AS p
				ON s.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.name = ? AND s.canonical = ?
		)
	`, tc, pn, sCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Secret: %w", err)
	}

	return nil
}

func scanSecret(s sqlr.Scanner) (*secret.Secret, error) {
	var sec dbSecret

	err := s.Scan(
		&sec.ID,
		&sec.Name,
		&sec.Type,
		&sec.Canonical,
		&sec.Params,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return sec.toDomainEntity(), nil
}

func scanSecrets(rows *sql.Rows) ([]*secret.Secret, error) {
	var ss []*secret.Secret

	for rows.Next() {
		s, err := scanSecret(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan secret: %w", err)
		}
		ss = append(ss, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan secret: %w", err)
	}
	return ss, nil
}
