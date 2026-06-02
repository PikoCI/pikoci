package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/job"
)

type JobRepository struct {
	querier sqlr.Querier
}

func NewJobRepository(db sqlr.Querier) *JobRepository {
	return &JobRepository{
		querier: db,
	}
}

type dbJob struct {
	ID          sql.NullInt64
	Name        sql.NullString
	Plan        sql.NullString
	OnSuccess   sql.NullString
	OnFailure   sql.NullString
	OnCancel    sql.NullString
	Ensure      sql.NullString
	Concurrency sql.NullInt64
	Paused      sql.NullBool
	Timeout     sql.NullInt64
}

func newDBJob(p job.Job) dbJob {
	pl, _ := json.Marshal(p.Plan)
	s, _ := json.Marshal(p.OnSuccess)
	f, _ := json.Marshal(p.OnFailure)
	c, _ := json.Marshal(p.OnCancel)
	e, _ := json.Marshal(p.Ensure)
	dbj := dbJob{
		Name:        toNullString(p.Name),
		Plan:        toNullString(string(pl)),
		OnSuccess:   toNullString(string(s)),
		OnFailure:   toNullString(string(f)),
		OnCancel:    toNullString(string(c)),
		Ensure:      toNullString(string(e)),
		Concurrency: sql.NullInt64{Int64: int64(p.Concurrency), Valid: true},
		Paused:      sql.NullBool{Bool: p.Paused, Valid: true},
	}
	if p.Timeout > 0 {
		dbj.Timeout = sql.NullInt64{Int64: int64(p.Timeout), Valid: true}
	}
	return dbj
}

func (dbp *dbJob) toDomainEntity() *job.Job {
	j := &job.Job{
		ID:          uint32(dbp.ID.Int64),
		Name:        dbp.Name.String,
		Concurrency: int(dbp.Concurrency.Int64),
		Paused:      dbp.Paused.Bool,
	}
	if dbp.Timeout.Valid {
		j.Timeout = time.Duration(dbp.Timeout.Int64)
	}

	_ = json.Unmarshal([]byte(dbp.Plan.String), &j.Plan)
	_ = json.Unmarshal([]byte(dbp.OnSuccess.String), &j.OnSuccess)
	_ = json.Unmarshal([]byte(dbp.OnFailure.String), &j.OnFailure)
	_ = json.Unmarshal([]byte(dbp.OnCancel.String), &j.OnCancel)
	_ = json.Unmarshal([]byte(dbp.Ensure.String), &j.Ensure)

	return j
}

func (r *JobRepository) Create(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
	dbj := newDBJob(j)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO jobs(name, plan, on_success, on_failure, on_cancel, ensure, concurrency, paused, timeout, pipeline_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbj.Name, dbj.Plan, dbj.OnSuccess, dbj.OnFailure, dbj.OnCancel, dbj.Ensure, dbj.Concurrency, dbj.Paused, dbj.Timeout, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *JobRepository) Update(ctx context.Context, tc, pn, jn string, j job.Job) error {
	dbj := newDBJob(j)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE jobs AS j
		SET name = ?, plan = ?, on_success = ?, on_failure = ?, on_cancel = ?, ensure = ?, concurrency = ?, paused = ?, timeout = ?
		FROM (
			SELECT j.id
			FROM jobs AS j
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
		) AS jj
		WHERE jj.id = j.id
	`, dbj.Name, dbj.Plan, dbj.OnSuccess, dbj.OnFailure, dbj.OnCancel, dbj.Ensure, dbj.Concurrency, dbj.Paused, dbj.Timeout, tc, pn, jn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	return nil
}

func (r *JobRepository) Find(ctx context.Context, tc, pn, jn string) (*job.Job, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT j.id, j.name, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure, j.concurrency, j.paused, j.timeout
		FROM jobs AS j
		JOIN pipelines AS p
			ON j.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
	`, tc, pn, jn)

	j, err := scanJob(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Job: %w", err)
	}

	return j, nil
}

func (r *JobRepository) Filter(ctx context.Context, tc, pn string) ([]*job.Job, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT j.id, j.name, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure, j.concurrency, j.paused, j.timeout
		FROM jobs AS j
		JOIN pipelines AS p
			ON j.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter jobs: %w", err)
	}

	jobs, err := scanJobs(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter jobs: %w", err)
	}

	return jobs, nil
}

func (r *JobRepository) Delete(ctx context.Context, tc, pn, jn string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM jobs
		WHERE id IN (
			SELECT j.id
			FROM jobs AS j
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
		)
	`, tc, pn, jn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Job: %w", err)
	}

	return nil
}

func (r *JobRepository) SetPaused(ctx context.Context, tc, pn, jn string, paused bool) error {
	res, err := r.querier.ExecContext(ctx, `
		UPDATE jobs AS j
		SET paused = ?
		FROM (
			SELECT j.id
			FROM jobs AS j
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
		) AS jj
		WHERE j.id = jj.id
	`, paused, tc, pn, jn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to set paused on Job: %w", err)
	}

	return nil
}

func (r *JobRepository) PauseAll(ctx context.Context, tc, pn string) error {
	_, err := r.querier.ExecContext(ctx, `
		UPDATE jobs AS j
		SET paused = TRUE
		FROM (
			SELECT j.id
			FROM jobs AS j
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ?
		) AS jj
		WHERE j.id = jj.id
	`, tc, pn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}
	return nil
}

func (r *JobRepository) UnpauseAll(ctx context.Context, tc, pn string) error {
	_, err := r.querier.ExecContext(ctx, `
		UPDATE jobs AS j
		SET paused = FALSE
		FROM (
			SELECT j.id
			FROM jobs AS j
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ?
		) AS jj
		WHERE j.id = jj.id
	`, tc, pn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}
	return nil
}

func scanJob(s sqlr.Scanner) (*job.Job, error) {
	var j dbJob

	err := s.Scan(
		&j.ID,
		&j.Name,
		&j.Plan,
		&j.OnSuccess,
		&j.OnFailure,
		&j.OnCancel,
		&j.Ensure,
		&j.Concurrency,
		&j.Paused,
		&j.Timeout,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return j.toDomainEntity(), nil
}

func scanJobs(rows *sql.Rows) ([]*job.Job, error) {
	var js []*job.Job

	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan job: %w", err)
		}
		js = append(js, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan job: %w", err)
	}
	return js, nil
}
