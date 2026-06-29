package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
	ID           sql.NullInt64
	Name         sql.NullString
	Tags         sql.NullString
	Plan         sql.NullString
	OnSuccess    sql.NullString
	OnFailure    sql.NullString
	OnCancel     sql.NullString
	Ensure       sql.NullString
	Concurrency  sql.NullInt64
	Paused       sql.NullBool
	Timeout      sql.NullInt64
	ForEachGroup      sql.NullString
	ForEachKey        sql.NullString
	BaselineVersionID sql.NullInt64
	ApproveLabel      sql.NullString
	ApproveTimeout    sql.NullString
	ApproveCount      sql.NullInt64
}

func newDBJob(p job.Job) dbJob {
	pl, _ := json.Marshal(p.Plan)
	s, _ := json.Marshal(p.OnSuccess)
	f, _ := json.Marshal(p.OnFailure)
	c, _ := json.Marshal(p.OnCancel)
	e, _ := json.Marshal(p.Ensure)
	dbj := dbJob{
		Name:         toNullString(p.Name),
		Tags:         sql.NullString{String: strings.Join(p.Tags, ","), Valid: true},
		Plan:         toNullString(string(pl)),
		OnSuccess:    toNullString(string(s)),
		OnFailure:    toNullString(string(f)),
		OnCancel:     toNullString(string(c)),
		Ensure:       toNullString(string(e)),
		Concurrency:  sql.NullInt64{Int64: int64(p.Concurrency), Valid: true},
		Paused:       sql.NullBool{Bool: p.Paused, Valid: true},
		ForEachGroup: toNullString(p.ForEachGroup),
		ForEachKey:   toNullString(p.ForEachKey),
	}
	if p.Timeout > 0 {
		dbj.Timeout = sql.NullInt64{Int64: int64(p.Timeout), Valid: true}
	}
	dbj.ApproveLabel = toNullString(p.ApproveLabel)
	dbj.ApproveCount = sql.NullInt64{Int64: int64(p.ApproveCount), Valid: true}
	return dbj
}

func (dbp *dbJob) toDomainEntity() *job.Job {
	var tags []string
	if dbp.Tags.String != "" {
		tags = strings.Split(dbp.Tags.String, ",")
	}
	j := &job.Job{
		ID:           uint32(dbp.ID.Int64),
		Name:         dbp.Name.String,
		Tags:         tags,
		Concurrency:  int(dbp.Concurrency.Int64),
		Paused:       dbp.Paused.Bool,
		ForEachGroup: dbp.ForEachGroup.String,
		ForEachKey:   dbp.ForEachKey.String,
	}
	if dbp.Timeout.Valid {
		j.Timeout = time.Duration(dbp.Timeout.Int64)
	}

	if dbp.BaselineVersionID.Valid {
		v := uint32(dbp.BaselineVersionID.Int64)
		j.BaselineVersionID = &v
	}
	if dbp.ApproveLabel.Valid && dbp.ApproveLabel.String != "" {
		j.ApproveLabel = dbp.ApproveLabel.String
		j.ApproveCount = int(dbp.ApproveCount.Int64)
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
		INSERT INTO jobs(name, tags, plan, on_success, on_failure, on_cancel, ensure, concurrency, paused, timeout, for_each_group, for_each_key, pipeline_id, baseline_version_id, approve_label, approve_timeout, approve_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			),
			-- baseline_version_id
			(SELECT MAX(id) FROM resource_versions), ?, ?, ?)`, dbj.Name, dbj.Tags, dbj.Plan, dbj.OnSuccess, dbj.OnFailure, dbj.OnCancel, dbj.Ensure, dbj.Concurrency, dbj.Paused, dbj.Timeout, dbj.ForEachGroup, dbj.ForEachKey, tc, pn, dbj.ApproveLabel, dbj.ApproveTimeout, dbj.ApproveCount)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	if err := r.replaceSerialGroups(ctx, id, j.SerialGroups); err != nil {
		return 0, fmt.Errorf("failed to insert serial groups: %w", err)
	}

	return id, nil
}

func (r *JobRepository) Update(ctx context.Context, tc, pn, jn string, j job.Job) error {
	dbj := newDBJob(j)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE jobs AS j
		SET name = ?, tags = ?, plan = ?, on_success = ?, on_failure = ?, on_cancel = ?, ensure = ?, concurrency = ?, timeout = ?, for_each_group = ?, for_each_key = ?, approve_label = ?, approve_timeout = ?, approve_count = ?
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
	`, dbj.Name, dbj.Tags, dbj.Plan, dbj.OnSuccess, dbj.OnFailure, dbj.OnCancel, dbj.Ensure, dbj.Concurrency, dbj.Timeout, dbj.ForEachGroup, dbj.ForEachKey, dbj.ApproveLabel, dbj.ApproveTimeout, dbj.ApproveCount, tc, pn, jn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	// Look up the job ID to replace serial groups.
	var jobID uint32
	err = r.querier.QueryRowContext(ctx, `
		SELECT j.id FROM jobs AS j
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
	`, tc, pn, j.Name).Scan(&jobID)
	if err != nil {
		return fmt.Errorf("failed to find job id for serial groups: %w", err)
	}

	if err := r.replaceSerialGroups(ctx, jobID, j.SerialGroups); err != nil {
		return fmt.Errorf("failed to update serial groups: %w", err)
	}

	return nil
}

func (r *JobRepository) Find(ctx context.Context, tc, pn, jn string) (*job.Job, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT j.id, j.name, j.tags, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure, j.concurrency, j.paused, j.timeout, j.for_each_group, j.for_each_key, j.baseline_version_id, j.approve_label, j.approve_timeout, j.approve_count
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

	sgs, err := r.loadSerialGroups(ctx, j.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load serial groups: %w", err)
	}
	j.SerialGroups = sgs

	return j, nil
}

func (r *JobRepository) Filter(ctx context.Context, tc, pn string) ([]*job.Job, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT j.id, j.name, j.tags, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure, j.concurrency, j.paused, j.timeout, j.for_each_group, j.for_each_key, j.baseline_version_id, j.approve_label, j.approve_timeout, j.approve_count
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

	for _, j := range jobs {
		sgs, err := r.loadSerialGroups(ctx, j.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load serial groups for job %q: %w", j.Name, err)
		}
		j.SerialGroups = sgs
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
		&j.Tags,
		&j.Plan,
		&j.OnSuccess,
		&j.OnFailure,
		&j.OnCancel,
		&j.Ensure,
		&j.Concurrency,
		&j.Paused,
		&j.Timeout,
		&j.ForEachGroup,
		&j.ForEachKey,
		&j.BaselineVersionID,
		&j.ApproveLabel,
		&j.ApproveTimeout,
		&j.ApproveCount,
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

func (r *JobRepository) replaceSerialGroups(ctx context.Context, jobID uint32, groups []string) error {
	_, err := r.querier.ExecContext(ctx, `DELETE FROM job_serial_groups WHERE job_id = ?`, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete old serial groups: %w", err)
	}
	for _, sg := range groups {
		_, err := r.querier.ExecContext(ctx, `INSERT INTO job_serial_groups (job_id, serial_group) VALUES (?, ?)`, jobID, sg)
		if err != nil {
			return fmt.Errorf("failed to insert serial group %q: %w", sg, err)
		}
	}
	return nil
}

func (r *JobRepository) loadSerialGroups(ctx context.Context, jobID uint32) ([]string, error) {
	rows, err := r.querier.QueryContext(ctx, `SELECT serial_group FROM job_serial_groups WHERE job_id = ? ORDER BY serial_group`, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query serial groups: %w", err)
	}
	defer rows.Close()

	var groups []string
	for rows.Next() {
		var sg string
		if err := rows.Scan(&sg); err != nil {
			return nil, fmt.Errorf("failed to scan serial group: %w", err)
		}
		groups = append(groups, sg)
	}
	return groups, rows.Err()
}

func (r *JobRepository) FilterByForEachGroup(ctx context.Context, tc, pn, group string) ([]*job.Job, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT j.id, j.name, j.tags, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure, j.concurrency, j.paused, j.timeout, j.for_each_group, j.for_each_key, j.baseline_version_id, j.approve_label, j.approve_timeout, j.approve_count
		FROM jobs AS j
		JOIN pipelines AS p
			ON j.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.for_each_group = ?
	`, tc, pn, group)
	if err != nil {
		return nil, fmt.Errorf("failed to filter jobs by for_each group: %w", err)
	}

	jobs, err := scanJobs(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan jobs by for_each group: %w", err)
	}

	for _, j := range jobs {
		sgs, err := r.loadSerialGroups(ctx, j.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load serial groups for job %q: %w", j.Name, err)
		}
		j.SerialGroups = sgs
	}

	return jobs, nil
}

func (r *JobRepository) FindJobsBySerialGroups(ctx context.Context, tc, pn string, serialGroups []string) ([]*job.Job, error) {
	if len(serialGroups) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(serialGroups))
	args := make([]interface{}, 0, len(serialGroups)+2)
	args = append(args, tc, pn)
	for i, sg := range serialGroups {
		placeholders[i] = "?"
		args = append(args, sg)
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT j.id, j.name, j.tags, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure, j.concurrency, j.paused, j.timeout, j.for_each_group, j.for_each_key, j.baseline_version_id, j.approve_label, j.approve_timeout, j.approve_count
		FROM jobs AS j
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		JOIN job_serial_groups AS sg ON j.id = sg.job_id
		WHERE t.canonical = ? AND p.canonical = ? AND sg.serial_group IN (%s)
	`, strings.Join(placeholders, ","))
	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to find jobs by serial groups: %w", err)
	}

	jobs, err := scanJobs(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan jobs by serial groups: %w", err)
	}

	for _, j := range jobs {
		sgs, err := r.loadSerialGroups(ctx, j.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load serial groups for job %q: %w", j.Name, err)
		}
		j.SerialGroups = sgs
	}

	return jobs, nil
}

