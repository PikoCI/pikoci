package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/sectype"
)

type SecretTypeRepository struct {
	querier sqlr.Querier
}

func NewSecretTypeRepository(db sqlr.Querier) *SecretTypeRepository {
	return &SecretTypeRepository{
		querier: db,
	}
}

type dbSecretType struct {
	ID     sql.NullInt64
	Name   sql.NullString
	Source sql.NullString
	Params sql.NullString
	Config sql.NullString
	Get    sql.NullString
}

func newDBSecretType(st sectype.SecretType) dbSecretType {
	p, _ := json.Marshal(st.Params)
	c, _ := json.Marshal(st.Config)
	g, _ := json.Marshal(st.Get)
	return dbSecretType{
		Name:   toNullString(st.Name),
		Source: toNullString(st.Source),
		Params: toNullString(string(p)),
		Config: toNullString(string(c)),
		Get:    toNullString(string(g)),
	}
}

func (dbst *dbSecretType) toDomainEntity() *sectype.SecretType {
	st := &sectype.SecretType{
		ID:     uint32(dbst.ID.Int64),
		Name:   dbst.Name.String,
		Source: dbst.Source.String,
	}

	_ = json.Unmarshal([]byte(dbst.Params.String), &st.Params)
	_ = json.Unmarshal([]byte(dbst.Config.String), &st.Config)
	_ = json.Unmarshal([]byte(dbst.Get.String), &st.Get)

	return st
}

func (r *SecretTypeRepository) Create(ctx context.Context, tc, pn string, st sectype.SecretType) (uint32, error) {
	dbst := newDBSecretType(st)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO secret_types(name, source, get, params, config, pipeline_id)
		VALUES (?, ?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbst.Name, dbst.Source, dbst.Get, dbst.Params, dbst.Config, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *SecretTypeRepository) Update(ctx context.Context, tc, pn, stn string, st sectype.SecretType) error {
	dbst := newDBSecretType(st)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE secret_types
		SET name = ?, source = ?, get = ?, params = ?, config = ?
		WHERE id = (
			SELECT st.id
			FROM (SELECT * FROM secret_types) AS st
			JOIN pipelines AS p
				ON st.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND st.name = ?
		)
	`, dbst.Name, dbst.Source, dbst.Get, dbst.Params, dbst.Config, tc, pn, stn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update secret type: %w", err)
	}

	return nil
}

func (r *SecretTypeRepository) Find(ctx context.Context, tc, pn, stn string) (*sectype.SecretType, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT st.id, st.name, st.source, st.get, st.params, st.config
		FROM secret_types AS st
		JOIN pipelines AS p
			ON st.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND st.name = ?
	`, tc, pn, stn)

	st, err := scanSecretType(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan SecretType: %w", err)
	}

	return st, nil
}

func (r *SecretTypeRepository) Filter(ctx context.Context, tc, pn string) ([]*sectype.SecretType, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT st.id, st.name, st.source, st.get, st.params, st.config
		FROM secret_types AS st
		JOIN pipelines AS p
			ON st.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter SecretTypes: %w", err)
	}

	sts, err := scanSecretTypes(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter secret types: %w", err)
	}

	return sts, nil
}

func (r *SecretTypeRepository) Delete(ctx context.Context, tc, pn, stn string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM secret_types
		WHERE id IN (
			SELECT st.id
			FROM secret_types AS st
			JOIN pipelines AS p
				ON st.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND st.name = ?
		)
	`, tc, pn, stn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the SecretType: %w", err)
	}

	return nil
}

func scanSecretType(s sqlr.Scanner) (*sectype.SecretType, error) {
	var st dbSecretType

	err := s.Scan(
		&st.ID,
		&st.Name,
		&st.Source,
		&st.Get,
		&st.Params,
		&st.Config,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return st.toDomainEntity(), nil
}

func scanSecretTypes(rows *sql.Rows) ([]*sectype.SecretType, error) {
	var sts []*sectype.SecretType

	for rows.Next() {
		st, err := scanSecretType(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan secret type: %w", err)
		}
		sts = append(sts, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan secret type: %w", err)
	}
	return sts, nil
}
