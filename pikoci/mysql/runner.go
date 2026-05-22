package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/xescugc/pikoci/pikoci/runner"
)

type RunnerRepository struct {
	querier sqlr.Querier
}

func NewRunnerRepository(db sqlr.Querier) *RunnerRepository {
	return &RunnerRepository{
		querier: db,
	}
}

type dbRunner struct {
	ID     sql.NullInt64
	Name   sql.NullString
	Source sql.NullString
	Run    sql.NullString
}

func newDBRunner(ru runner.Runner) dbRunner {
	r, _ := json.Marshal(ru.Run)
	return dbRunner{
		Name:   toNullString(ru.Name),
		Source: toNullString(ru.Source),
		Run:    toNullString(string(r)),
	}
}

func (dbru *dbRunner) toDomainEntity() *runner.Runner {
	ru := &runner.Runner{
		ID:     uint32(dbru.ID.Int64),
		Name:   dbru.Name.String,
		Source: dbru.Source.String,
	}

	_ = json.Unmarshal([]byte(dbru.Run.String), &ru.Run)

	return ru
}

func (r *RunnerRepository) Create(ctx context.Context, tc, pn string, ru runner.Runner) (uint32, error) {
	dbru := newDBRunner(ru)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO runners(name, source, run, pipeline_id)
		VALUES (?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbru.Name, dbru.Source, dbru.Run, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *RunnerRepository) Update(ctx context.Context, tc, pn, run string, ru runner.Runner) error {
	dbru := newDBRunner(ru)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE runners AS ru
		SET name = ?, source = ?, run = ?
		FROM (
			SELECT ru.id
			FROM runners AS ru
			JOIN pipelines AS p
				ON ru.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND ru.name = ?
		) AS ruru
		WHERE ruru.id = ru.id
	`, dbru.Name, dbru.Source, dbru.Run, tc, pn, run)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update runner: %w", err)
	}

	return nil
}

func (r *RunnerRepository) Find(ctx context.Context, tc, pn, run string) (*runner.Runner, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT ru.id, ru.name, ru.source, ru.run
		FROM runners AS ru
		JOIN pipelines AS p
			ON ru.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND ru.name = ?
	`, tc, pn, run)

	ru, err := scanRunner(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Runner: %w", err)
	}

	return ru, nil
}

func (r *RunnerRepository) Filter(ctx context.Context, tc, pn string) ([]*runner.Runner, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT ru.id, ru.name, ru.source, ru.run
		FROM runners AS ru
		JOIN pipelines AS p
			ON ru.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter runners: %w", err)
	}

	runners, err := scanRunners(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter runners: %w", err)
	}

	return runners, nil
}

func (r *RunnerRepository) Delete(ctx context.Context, tc, pn, run string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM runners
		WHERE id IN (
			SELECT ru.id
			FROM runners AS ru
			JOIN pipelines AS p
				ON ru.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND ru.name = ?
		)
	`, tc, pn, run)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Runner: %w", err)
	}

	return nil
}

func scanRunner(s sqlr.Scanner) (*runner.Runner, error) {
	var ru dbRunner

	err := s.Scan(
		&ru.ID,
		&ru.Name,
		&ru.Source,
		&ru.Run,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return ru.toDomainEntity(), nil
}

func scanRunners(rows *sql.Rows) ([]*runner.Runner, error) {
	var rus []*runner.Runner

	for rows.Next() {
		ru, err := scanRunner(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan runner: %w", err)
		}
		rus = append(rus, ru)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan runner: %w", err)
	}
	return rus, nil
}
