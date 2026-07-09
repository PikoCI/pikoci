package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/build"
)

type BuildRepository struct {
	querier sqlr.Querier
	system  string
}

func NewBuildRepository(db sqlr.Querier, system string) *BuildRepository {
	return &BuildRepository{
		querier: db,
		system:  system,
	}
}

// castAsInt returns a SQL expression that casts expr to an integer,
// portable across SQLite, PostgreSQL, and MySQL.
func (r *BuildRepository) castAsInt(expr string) string {
	if r.system == MySQL {
		return "CAST(" + expr + " AS SIGNED)"
	}
	return "CAST(" + expr + " AS INTEGER)"
}

type dbBuild struct {
	ID                sql.NullInt64
	BuildNumber       sql.NullString
	Steps             sql.NullString
	Job               sql.NullString
	Status            sql.NullString
	Error             sql.NullString
	StartedAt         sql.NullTime
	Duration          sql.NullInt64
	VersionID            sql.NullInt64
	ResourceCanonical    sql.NullString
	RetrySourceBuildID   int64
}

func newDBBuild(b build.Build) dbBuild {
	s, _ := json.Marshal(b.Steps)
	j, _ := json.Marshal(b.Job)
	return dbBuild{
		Steps:             toNullString(string(s)),
		Job:               toNullString(string(j)),
		Status:            toNullString(b.Status.String()),
		Error:             toNullString(b.Error),
		StartedAt:         toNullTime(b.StartedAt),
		Duration:          toNullInt64(int(b.Duration)),
		VersionID:          toNullInt64(int(b.VersionID)),
		ResourceCanonical:  toNullString(b.ResourceCanonical),
		RetrySourceBuildID: int64(b.RetrySourceBuildID),
	}
}

func (dbb *dbBuild) toDomainEntity() *build.Build {
	s, _ := build.StatusString(dbb.Status.String)
	b := &build.Build{
		ID:                uint32(dbb.ID.Int64),
		BuildNumber:       dbb.BuildNumber.String,
		Status:            s,
		Error:             dbb.Error.String,
		StartedAt:         dbb.StartedAt.Time,
		Duration:          time.Duration(dbb.Duration.Int64),
		VersionID:          uint32(dbb.VersionID.Int64),
		ResourceCanonical:  dbb.ResourceCanonical.String,
		RetrySourceBuildID: uint32(dbb.RetrySourceBuildID),
	}

	_ = json.Unmarshal([]byte(dbb.Steps.String), &b.Steps)
	_ = json.Unmarshal([]byte(dbb.Job.String), &b.Job)

	return b
}

func (r *BuildRepository) Create(ctx context.Context, tc, pn, jn string, b build.Build) (uint32, string, error) {
	dbb := newDBBuild(b)

	// Retry loop to handle concurrent builds for the same job.
	// The UNIQUE index on (job_id, build_number) prevents duplicates;
	// on conflict we re-read the max and try the next number.
	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		var maxNum sql.NullInt64
		err := r.querier.QueryRowContext(ctx, `
			SELECT MAX(`+r.castAsInt("b.build_number")+`)
			FROM builds AS b
			JOIN jobs AS j ON b.job_id = j.id
			JOIN pipelines AS p ON j.pipeline_id = p.id
			JOIN teams AS t ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
			  AND b.build_number NOT LIKE '%.%'
		`, tc, pn, jn).Scan(&maxNum)
		if err != nil {
			return 0, "", fmt.Errorf("failed to query max build number: %w", err)
		}
		nextNum := uint32(1)
		if maxNum.Valid {
			nextNum = uint32(maxNum.Int64) + 1
		}
		buildNumber := fmt.Sprintf("%d", nextNum)

		res, err := r.querier.ExecContext(ctx, `
			INSERT INTO builds(steps, job, status, error, started_at, duration, build_number, version_id, resource_canonical, retry_source_build_id, job_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				-- job_id
				(
					SELECT j.id
					FROM jobs AS j
					JOIN pipelines AS p
						ON j.pipeline_id = p.id
					JOIN teams AS t
						ON p.team_id = t.id
					WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
				))`, dbb.Steps, dbb.Job, dbb.Status, dbb.Error, dbb.StartedAt, dbb.Duration, buildNumber, dbb.VersionID, dbb.ResourceCanonical, dbb.RetrySourceBuildID, tc, pn, jn)
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return 0, "", fmt.Errorf("failed to execute query: %w", err)
		}

		id, err := lastInsertedID(res)
		if err != nil {
			return 0, "", fmt.Errorf("failed to get last inserted id: %w", err)
		}

		return id, buildNumber, nil
	}

	return 0, "", fmt.Errorf("failed to allocate build number after %d retries", maxRetries)
}

func (r *BuildRepository) CreateRetry(ctx context.Context, tc, pn, jn, parentBuildNumber string, b build.Build) (uint32, string, error) {
	dbb := newDBBuild(b)

	likePattern := parentBuildNumber + ".%"
	// SUBSTR offset: skip parent number + dot
	substrOffset := len(parentBuildNumber) + 2

	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		var maxNum sql.NullInt64
		err := r.querier.QueryRowContext(ctx, `
			SELECT MAX(`+r.castAsInt(fmt.Sprintf("SUBSTR(b.build_number, %d)", substrOffset))+`)
			FROM builds AS b
			JOIN jobs AS j ON b.job_id = j.id
			JOIN pipelines AS p ON j.pipeline_id = p.id
			JOIN teams AS t ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
			  AND b.build_number LIKE ?
		`, tc, pn, jn, likePattern).Scan(&maxNum)
		if err != nil {
			return 0, "", fmt.Errorf("failed to query max retry build number: %w", err)
		}
		nextNum := uint32(1)
		if maxNum.Valid {
			nextNum = uint32(maxNum.Int64) + 1
		}
		buildNumber := fmt.Sprintf("%s.%d", parentBuildNumber, nextNum)

		res, err := r.querier.ExecContext(ctx, `
			INSERT INTO builds(steps, job, status, error, started_at, duration, build_number, version_id, resource_canonical, retry_source_build_id, job_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				(
					SELECT j.id
					FROM jobs AS j
					JOIN pipelines AS p
						ON j.pipeline_id = p.id
					JOIN teams AS t
						ON p.team_id = t.id
					WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
				))`, dbb.Steps, dbb.Job, dbb.Status, dbb.Error, dbb.StartedAt, dbb.Duration, buildNumber, dbb.VersionID, dbb.ResourceCanonical, dbb.RetrySourceBuildID, tc, pn, jn)
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return 0, "", fmt.Errorf("failed to execute query: %w", err)
		}

		id, err := lastInsertedID(res)
		if err != nil {
			return 0, "", fmt.Errorf("failed to get last inserted id: %w", err)
		}

		return id, buildNumber, nil
	}

	return 0, "", fmt.Errorf("failed to allocate retry build number after %d retries", maxRetries)
}

func (r *BuildRepository) FindGetVersions(ctx context.Context, buildID uint32) (map[string]uint32, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT step_name, version_id FROM build_get_versions WHERE build_id = ?
	`, buildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query build get versions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint32)
	for rows.Next() {
		var stepName string
		var versionID uint32
		if err := rows.Scan(&stepName, &versionID); err != nil {
			return nil, fmt.Errorf("failed to scan build get version: %w", err)
		}
		result[stepName] = versionID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate build get versions: %w", err)
	}
	return result, nil
}

func (r *BuildRepository) Find(ctx context.Context, tc, pn, jn string, buildNumber string) (*build.Build, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT b.id, b.build_number, b.steps, b.job, b.status, b.error, b.started_at, b.duration, b.version_id, b.resource_canonical, b.retry_source_build_id
		FROM builds AS b
		JOIN jobs AS j
			ON b.job_id = j.id
		JOIN pipelines AS p
			ON j.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.name = ? AND b.build_number = ?
	`, tc, pn, jn, buildNumber)

	j, err := scanBuild(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Build: %w", err)
	}

	return j, nil
}

func (r *BuildRepository) Filter(ctx context.Context, tc, pn, jn string, before *uint32, after *uint32, limit uint32, statuses []build.Status) ([]*build.Build, error) {
	query := `
		SELECT b.id, b.build_number, b.steps, b.job, b.status, b.error, b.started_at, b.duration, b.version_id, b.resource_canonical, b.retry_source_build_id
		FROM builds AS b
		JOIN jobs AS j
			ON b.job_id = j.id
		JOIN pipelines AS p
			ON j.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?`
	args := []interface{}{tc, pn, jn}

	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, s := range statuses {
			placeholders[i] = "?"
			args = append(args, s.String())
		}
		query += ` AND b.status IN (` + strings.Join(placeholders, ", ") + `)`
	}

	if after != nil {
		query += ` AND b.id > ?`
		args = append(args, *after)
		query += ` ORDER BY b.id ASC`
	} else if before != nil {
		query += ` AND b.id < ?`
		args = append(args, *before)
		query += ` ORDER BY b.id DESC`
		if limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, limit)
		}
	} else {
		// Initial load or limit=0 (all)
		query += ` ORDER BY b.id DESC`
		if limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, limit)
		}
	}

	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to filter builds: %w", err)
	}

	builds, err := scanBuilds(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter builds: %w", err)
	}

	return builds, nil
}

func (r *BuildRepository) Update(ctx context.Context, tc, pn, jn string, buildNumber string, b build.Build) error {
	dbb := newDBBuild(b)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE builds AS b
		SET steps = ?, job = ?, status = ?, error = ?, started_at = ?, duration = ?, version_id = ?, resource_canonical = ?
		FROM (
			SELECT b.id
			FROM builds AS b
			JOIN jobs AS j
				ON b.job_id = j.id
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ? AND b.build_number = ?
		) AS bb
		WHERE bb.id = b.id
	`, dbb.Steps, dbb.Job, dbb.Status, dbb.Error, dbb.StartedAt, dbb.Duration, dbb.VersionID, dbb.ResourceCanonical, tc, pn, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update build: %w", err)
	}

	return nil
}

func (r *BuildRepository) Delete(ctx context.Context, tc, pn, jn string, buildNumber string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM builds
		WHERE id IN (
			SELECT b.id
			FROM builds AS b
			JOIN jobs AS j
				ON b.job_id = j.id
			JOIN pipelines AS p
				ON j.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND j.name = ? AND b.build_number = ?
		)
	`, tc, pn, jn, buildNumber)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Job: %w", err)
	}

	return nil
}

func (r *BuildRepository) InsertGetVersion(ctx context.Context, tc, pn, jn string, buildID uint32, stepName string, versionID uint32) error {
	_, err := r.querier.ExecContext(ctx, `
		INSERT OR IGNORE INTO build_get_versions(build_id, step_name, version_id)
		VALUES (?, ?, ?)
	`, buildID, stepName, versionID)
	if err != nil {
		return fmt.Errorf("failed to insert build get version: %w", err)
	}
	return nil
}

// FindReadyDownstreamVersion finds the highest version_id that ALL upstream
// jobs succeeded with but the downstream job has no build for yet (regardless
// of build status). Any existing build (pending, started, succeeded, failed,
// cancelled) for the version on the downstream job prevents re-triggering.
func (r *BuildRepository) FindReadyDownstreamVersion(ctx context.Context, tc, pn string, upstreamJobs []string, downstreamJob string, stepName string, upstreamCount int, baselineVersionID *uint32) (uint32, bool, error) {
	// Build the IN clause placeholders
	placeholders := make([]string, len(upstreamJobs))
	args := make([]interface{}, 0, len(upstreamJobs)+5)
	args = append(args, tc, pn)
	for i, j := range upstreamJobs {
		placeholders[i] = "?"
		args = append(args, j)
	}
	args = append(args, stepName)
	// Args for the NOT EXISTS subquery
	args = append(args, downstreamJob)
	// HAVING count
	args = append(args, upstreamCount)

	baselineClause := ""
	if baselineVersionID != nil {
		baselineClause = fmt.Sprintf("AND bgv.version_id > %d", *baselineVersionID)
	}

	query := `
		SELECT bgv.version_id
		FROM build_get_versions bgv
		JOIN builds b ON bgv.build_id = b.id
		JOIN jobs j ON b.job_id = j.id
		JOIN pipelines p ON j.pipeline_id = p.id
		JOIN teams t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND b.status IN ('succeeded', 'warning')
		  AND j.name IN (` + strings.Join(placeholders, ", ") + `)
		  AND bgv.step_name = ?
		  ` + baselineClause + `
		  AND NOT EXISTS (
			  SELECT 1 FROM builds b2
			  JOIN jobs j2 ON b2.job_id = j2.id
			  WHERE j2.pipeline_id = p.id AND j2.name = ?
				AND b2.version_id = bgv.version_id
		  )
		GROUP BY bgv.version_id
		HAVING COUNT(DISTINCT j.name) = ?
		ORDER BY bgv.version_id DESC
		LIMIT 1
	`

	var versionID uint32
	err := r.querier.QueryRowContext(ctx, query, args...).Scan(&versionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to find ready downstream version: %w", err)
	}
	return versionID, true, nil
}

func (r *BuildRepository) LastBuildAtByPipeline(ctx context.Context, tc string) (map[uint32]time.Time, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT p.id, MAX(b.started_at)
		FROM builds AS b
		JOIN jobs AS j ON b.job_id = j.id
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ?
		GROUP BY p.id
	`, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to query last build timestamps: %w", err)
	}
	defer rows.Close()

	result := make(map[uint32]time.Time)
	for rows.Next() {
		var pipelineID uint32
		var startedAtStr sql.NullString
		if err := rows.Scan(&pipelineID, &startedAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan last build timestamp: %w", err)
		}
		if startedAtStr.Valid && startedAtStr.String != "" {
			s := startedAtStr.String
			// Strip Go monotonic clock suffix (e.g. " m=+123.456")
			if idx := strings.Index(s, " m="); idx != -1 {
				s = strings.TrimSpace(s[:idx])
			}
			var t time.Time
			var parseErr error
			for _, layout := range []string{
				time.RFC3339,
				"2006-01-02 15:04:05.999999999 +0000 UTC",
				"2006-01-02 15:04:05 -0700 MST",
				"2006-01-02 15:04:05",
				"2006-01-02T15:04:05Z",
			} {
				t, parseErr = time.Parse(layout, s)
				if parseErr == nil {
					break
				}
			}
			if parseErr == nil {
				result[pipelineID] = t
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate last build timestamps: %w", err)
	}

	return result, nil
}

func (r *BuildRepository) CountRunning(ctx context.Context, tc, pn, jn string) (int, error) {
	var count int
	err := r.querier.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM builds AS b
		JOIN jobs AS j ON b.job_id = j.id
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.name = ?
		  AND b.status = 'started'
	`, tc, pn, jn).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count running builds: %w", err)
	}
	return count, nil
}

func (r *BuildRepository) CountRunningInSerialGroups(ctx context.Context, tc, pn string, serialGroups []string, excludeJobName string) (int, error) {
	if len(serialGroups) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(serialGroups))
	args := make([]interface{}, 0, len(serialGroups)+3)
	args = append(args, tc, pn)
	for i, sg := range serialGroups {
		placeholders[i] = "?"
		args = append(args, sg)
	}
	args = append(args, excludeJobName)
	query := fmt.Sprintf(`
		SELECT COUNT(DISTINCT b.id)
		FROM builds AS b
		JOIN jobs AS j ON b.job_id = j.id
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		JOIN job_serial_groups AS sg ON j.id = sg.job_id
		WHERE t.canonical = ? AND p.canonical = ?
		  AND sg.serial_group IN (%s)
		  AND b.status = 'started'
		  AND j.name != ?
	`, strings.Join(placeholders, ","))
	var count int
	err := r.querier.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count running builds in serial groups: %w", err)
	}
	return count, nil
}

func (r *BuildRepository) FindByID(ctx context.Context, buildID uint32) (*build.Build, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT b.id, b.build_number, b.steps, b.job, b.status, b.error, b.started_at, b.duration, b.version_id, b.resource_canonical, b.retry_source_build_id
		FROM builds AS b
		WHERE b.id = ?
	`, buildID)

	b, err := scanBuild(row)
	if err != nil {
		return nil, fmt.Errorf("failed to find build by ID: %w", err)
	}
	return b, nil
}

func (r *BuildRepository) FindOldestPending(ctx context.Context, tc, pn, jn string) (*build.Build, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT b.id, b.build_number, b.steps, b.job, b.status, b.error, b.started_at, b.duration, b.version_id, b.resource_canonical, b.retry_source_build_id
		FROM builds AS b
		JOIN jobs AS j
			ON b.job_id = j.id
		JOIN pipelines AS p
			ON j.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND j.name = ? AND b.status = 'pending'
		ORDER BY b.id ASC
		LIMIT 1
	`, tc, pn, jn)

	b, err := scanBuild(row)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find oldest pending build: %w", err)
	}
	return b, nil
}

func (r *BuildRepository) StartPending(ctx context.Context, tc, pn, jn string, buildID uint32) error {
	now := time.Now().Round(0)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE builds
		SET status = 'started', started_at = ?
		WHERE id = ? AND status = 'pending'
	`, now, buildID)
	if err != nil {
		return fmt.Errorf("failed to start pending build: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("build %d is not in pending status: %w", buildID, build.ErrNotPending)
	}
	return nil
}

func (r *BuildRepository) AggregateStatusByVersionIDs(ctx context.Context, versionIDs []uint32) (map[uint32]string, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(versionIDs))
	args := make([]interface{}, len(versionIDs))
	for i, id := range versionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT bgv.version_id,
			CASE
				WHEN SUM(CASE WHEN b.status = 'failed' THEN 1 ELSE 0 END) > 0 THEN 'failed'
				WHEN SUM(CASE WHEN b.status = 'started' THEN 1 ELSE 0 END) > 0 THEN 'started'
				WHEN SUM(CASE WHEN b.status = 'pending' THEN 1 ELSE 0 END) > 0 THEN 'pending'
				WHEN SUM(CASE WHEN b.status = 'waiting_for_approval' THEN 1 ELSE 0 END) > 0 THEN 'waiting_for_approval'
				WHEN SUM(CASE WHEN b.status = 'succeeded' THEN 1 ELSE 0 END) > 0 THEN 'succeeded'
				WHEN SUM(CASE WHEN b.status = 'cancelled' THEN 1 ELSE 0 END) > 0 THEN 'cancelled'
				ELSE ''
			END AS agg_status
		FROM build_get_versions bgv
		JOIN builds b ON b.id = bgv.build_id
		WHERE bgv.version_id IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY bgv.version_id
	`

	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregate status by version IDs: %w", err)
	}
	defer rows.Close()

	result := make(map[uint32]string)
	for rows.Next() {
		var versionID uint32
		var status string
		if err := rows.Scan(&versionID, &status); err != nil {
			return nil, fmt.Errorf("failed to scan aggregate status: %w", err)
		}
		if status != "" {
			result[versionID] = status
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate aggregate status: %w", err)
	}

	return result, nil
}

func (r *BuildRepository) FindByVersionAndJobs(ctx context.Context, tc, pn string, versionID uint32, jobNames []string) (map[string][]*build.Build, error) {
	if len(jobNames) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(jobNames))
	args := make([]interface{}, 0, len(jobNames)+3)
	args = append(args, tc, pn)
	for i, jn := range jobNames {
		placeholders[i] = "?"
		args = append(args, jn)
	}
	args = append(args, versionID, versionID)

	// Find builds that consumed this version, plus any retries of those builds.
	// Retries may not have build_get_versions entries yet (pending/started),
	// so we also match on retry_source_build_id.
	query := `
		SELECT b.id, b.build_number, b.steps, b.job, b.status, b.error, b.started_at, b.duration, b.version_id, b.resource_canonical, b.retry_source_build_id, j.name
		FROM builds AS b
		JOIN jobs AS j ON b.job_id = j.id
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
		  AND j.name IN (` + strings.Join(placeholders, ",") + `)
		  AND (
			b.id IN (
				SELECT bgv.build_id
				FROM build_get_versions bgv
				WHERE bgv.version_id = ?
			)
			OR b.retry_source_build_id IN (
				SELECT bgv.build_id
				FROM build_get_versions bgv
				WHERE bgv.version_id = ?
			)
		  )
		ORDER BY j.name, b.id DESC
	`

	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query builds by version and jobs: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]*build.Build)
	for rows.Next() {
		var dbb dbBuild
		var jobName string
		err := rows.Scan(
			&dbb.ID,
			&dbb.BuildNumber,
			&dbb.Steps,
			&dbb.Job,
			&dbb.Status,
			&dbb.Error,
			&dbb.StartedAt,
			&dbb.Duration,
			&dbb.VersionID,
			&dbb.ResourceCanonical,
			&dbb.RetrySourceBuildID,
			&jobName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan build: %w", err)
		}
		result[jobName] = append(result[jobName], dbb.toDomainEntity())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate builds: %w", err)
	}

	// Reorder: main builds first (no dot in build number), then retries
	for jn, builds := range result {
		var main []*build.Build
		var retries []*build.Build
		for _, b := range builds {
			if strings.Contains(b.BuildNumber, ".") {
				retries = append(retries, b)
			} else {
				main = append(main, b)
			}
		}
		result[jn] = append(main, retries...)
	}

	return result, nil
}

func (r *BuildRepository) FilterByPipeline(ctx context.Context, tc, pn string, statuses []build.Status) (map[string][]*build.Build, error) {
	query := `
		SELECT j.name, b.id, b.build_number, b.steps, b.job, b.status, b.error, b.started_at, b.duration, b.version_id, b.resource_canonical, b.retry_source_build_id
		FROM builds AS b
		JOIN jobs AS j ON b.job_id = j.id
		JOIN pipelines AS p ON j.pipeline_id = p.id
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?`
	args := []interface{}{tc, pn}

	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, s := range statuses {
			placeholders[i] = "?"
			args = append(args, s.String())
		}
		query += ` AND b.status IN (` + strings.Join(placeholders, ", ") + `)`
	}

	query += ` ORDER BY b.id DESC`

	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to filter builds by pipeline: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]*build.Build)
	for rows.Next() {
		var dbb dbBuild
		var jobName string
		err := rows.Scan(
			&jobName,
			&dbb.ID,
			&dbb.BuildNumber,
			&dbb.Steps,
			&dbb.Job,
			&dbb.Status,
			&dbb.Error,
			&dbb.StartedAt,
			&dbb.Duration,
			&dbb.VersionID,
			&dbb.ResourceCanonical,
			&dbb.RetrySourceBuildID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan build: %w", err)
		}
		result[jobName] = append(result[jobName], dbb.toDomainEntity())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate builds by pipeline: %w", err)
	}

	return result, nil
}

func (r *BuildRepository) CountStarted(ctx context.Context) (int, error) {
	var count int
	err := r.querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM builds WHERE status = 'started'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count started builds: %w", err)
	}
	return count, nil
}

func (r *BuildRepository) FailStartedBuilds(ctx context.Context, reason string) (int, error) {
	res, err := r.querier.ExecContext(ctx, `UPDATE builds SET status = 'failed', error = ? WHERE status = 'started'`, reason)
	if err != nil {
		return 0, fmt.Errorf("failed to fail started builds: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return int(rows), nil
}

func scanBuild(s sqlr.Scanner) (*build.Build, error) {
	var b dbBuild

	err := s.Scan(
		&b.ID,
		&b.BuildNumber,
		&b.Steps,
		&b.Job,
		&b.Status,
		&b.Error,
		&b.StartedAt,
		&b.Duration,
		&b.VersionID,
		&b.ResourceCanonical,
		&b.RetrySourceBuildID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return b.toDomainEntity(), nil
}

func scanBuilds(rows *sql.Rows) ([]*build.Build, error) {
	var bs []*build.Build

	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan build: %w", err)
		}
		bs = append(bs, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan build: %w", err)
	}
	return bs, nil
}

func (r *BuildRepository) CreateApproval(ctx context.Context, buildID uint32, username, action, message string) error {
	_, err := r.querier.ExecContext(ctx, `
		INSERT INTO build_approvals (build_id, username, action, message)
		VALUES (?, ?, ?, ?)
	`, buildID, username, action, message)
	if err != nil {
		return fmt.Errorf("failed to create approval: %w", err)
	}
	return nil
}

func (r *BuildRepository) FindApprovals(ctx context.Context, buildID uint32) ([]build.Approval, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT id, build_id, username, action, message, created_at
		FROM build_approvals
		WHERE build_id = ?
		ORDER BY created_at ASC
	`, buildID)
	if err != nil {
		return nil, fmt.Errorf("failed to query approvals: %w", err)
	}
	defer rows.Close()

	var approvals []build.Approval
	for rows.Next() {
		var a build.Approval
		var msg sql.NullString
		if err := rows.Scan(&a.ID, &a.BuildID, &a.Username, &a.Action, &msg, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan approval: %w", err)
		}
		a.Message = msg.String
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}

func (r *BuildRepository) CountApprovals(ctx context.Context, buildID uint32) (int, error) {
	var count int
	err := r.querier.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM build_approvals
		WHERE build_id = ? AND action = 'approved'
	`, buildID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count approvals: %w", err)
	}
	return count, nil
}
