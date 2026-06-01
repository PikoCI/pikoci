package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/resource"
)

type ResourceRepository struct {
	querier sqlr.Querier
	system  string
}

func NewResourceRepository(db sqlr.Querier, system string) *ResourceRepository {
	return &ResourceRepository{
		querier: db,
		system:  system,
	}
}

type dbResource struct {
	ID            sql.NullInt64
	Name          sql.NullString
	Type          sql.NullString
	Canonical     sql.NullString
	Params        sql.NullString
	Logs          sql.NullString
	CheckInterval sql.NullString
	LastCheck     sql.NullTime
	NextCheck     sql.NullTime
	WebhookToken    sql.NullString
	Cache           sql.NullBool
	PinnedVersionID sql.NullInt64
}

type dbResourceVersion struct {
	ID      sql.NullInt64
	Version sql.NullString
}

func newDBResource(r resource.Resource) dbResource {
	i, _ := json.Marshal(r.Params)
	dbr := dbResource{
		Name:          toNullString(r.Name),
		Type:          toNullString(r.Type),
		Canonical:     toNullString(r.Canonical),
		Params:        toNullString(string(i)),
		Logs:          toNullString(r.Logs),
		CheckInterval: toNullString(r.CheckInterval),
		LastCheck:     toNullTime(r.LastCheck),
		NextCheck:     toNullTime(r.NextCheck),
		WebhookToken:  toNullString(r.WebhookToken),
	}
	if r.Cache != nil {
		dbr.Cache = sql.NullBool{Bool: *r.Cache, Valid: true}
	}
	return dbr
}

func (dbr *dbResource) toDomainEntity() *resource.Resource {
	r := &resource.Resource{
		ID:            uint32(dbr.ID.Int64),
		Name:          dbr.Name.String,
		Type:          dbr.Type.String,
		Canonical:     dbr.Canonical.String,
		Logs:          dbr.Logs.String,
		CheckInterval: dbr.CheckInterval.String,
		LastCheck:     dbr.LastCheck.Time,
		NextCheck:     dbr.NextCheck.Time,
		WebhookToken:  dbr.WebhookToken.String,
	}

	_ = json.Unmarshal([]byte(dbr.Params.String), &r.Params)

	if dbr.Cache.Valid {
		c := dbr.Cache.Bool
		r.Cache = &c
	}

	if dbr.PinnedVersionID.Valid {
		v := uint32(dbr.PinnedVersionID.Int64)
		r.PinnedVersionID = &v
	}

	return r
}

func newDBResourceVersion(v resource.Version) dbResourceVersion {
	vv, _ := json.Marshal(v.Version)
	return dbResourceVersion{
		Version: toNullString(string(vv)),
	}
}

func (dbrv *dbResourceVersion) toDomainEntity() *resource.Version {
	v := &resource.Version{
		ID: uint32(dbrv.ID.Int64),
	}
	_ = json.Unmarshal([]byte(dbrv.Version.String), &v.Version)

	return v
}

func (r *ResourceRepository) Create(ctx context.Context, tc, pn string, rs resource.Resource) (uint32, error) {
	dbrs := newDBResource(rs)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO resources(name, `+"`type`"+`, canonical, params, check_interval, last_check, next_check, webhook_token, cache, pipeline_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbrs.Name, dbrs.Type, dbrs.Canonical, dbrs.Params, dbrs.CheckInterval, dbrs.LastCheck, dbrs.NextCheck, dbrs.WebhookToken, dbrs.Cache, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *ResourceRepository) Update(ctx context.Context, tc, pn, rCan string, rs resource.Resource) error {
	dbrs := newDBResource(rs)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE resources AS r
		SET name = ?, type = ?, canonical = ?, params = ?, check_interval = ?, logs = ?, last_check = ?, next_check = ?, webhook_token = ?, cache = ?
		FROM (
			SELECT r.id
			FROM resources AS r
			JOIN pipelines AS p
				ON r.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?
		) AS rr
		WHERE rr.id = r.id
	`, dbrs.Name, dbrs.Type, dbrs.Canonical, dbrs.Params, dbrs.CheckInterval, dbrs.Logs, dbrs.LastCheck, dbrs.NextCheck, dbrs.WebhookToken, dbrs.Cache, tc, pn, rCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}

	return nil
}

func (r *ResourceRepository) Find(ctx context.Context, tc, pn, rCan string) (*resource.Resource, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT r.id, r.name, r.type, r.canonical, r.params, r.check_interval, r.logs, r.last_check, r.next_check, r.webhook_token, r.cache, r.pinned_version_id
		FROM resources AS r
		JOIN pipelines AS p
			ON r.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?
	`, tc, pn, rCan)

	rs, err := scanResource(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Resource: %w", err)
	}

	return rs, nil
}

func (r *ResourceRepository) FindByWebhookToken(ctx context.Context, token string) (*resource.Resource, string, string, error) {
	var tc, pn sql.NullString
	row := r.querier.QueryRowContext(ctx, `
		SELECT r.id, r.name, r.type, r.canonical, r.params, r.check_interval, r.logs, r.last_check, r.next_check, r.webhook_token, r.cache, r.pinned_version_id,
			t.canonical, p.canonical
		FROM resources AS r
		JOIN pipelines AS p
			ON r.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE r.webhook_token = ?
	`, token)

	var dbr dbResource
	err := row.Scan(
		&dbr.ID,
		&dbr.Name,
		&dbr.Type,
		&dbr.Canonical,
		&dbr.Params,
		&dbr.CheckInterval,
		&dbr.Logs,
		&dbr.LastCheck,
		&dbr.NextCheck,
		&dbr.WebhookToken,
		&dbr.Cache,
		&dbr.PinnedVersionID,
		&tc,
		&pn,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", "", fmt.Errorf("not found")
		}
		return nil, "", "", fmt.Errorf("failed to scan: %w", err)
	}

	return dbr.toDomainEntity(), tc.String, pn.String, nil
}

func (r *ResourceRepository) Filter(ctx context.Context, tc, pn string) ([]*resource.Resource, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT r.id, r.name, r.type, r.canonical, r.params, r.check_interval, r.logs, r.last_check, r.next_check, r.webhook_token, r.cache, r.pinned_version_id
		FROM resources AS r
		JOIN pipelines AS p
			ON r.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Resources: %w", err)
	}

	resources, err := scanResources(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter resources: %w", err)
	}

	return resources, nil
}

func (r *ResourceRepository) FilterDueResources(ctx context.Context) ([]*resource.ResourceWithPipeline, error) {
	q := `
		SELECT r.id, r.name, r.type, r.canonical, r.params, r.check_interval, r.logs, r.last_check, r.next_check, r.webhook_token, r.cache, r.pinned_version_id,
			t.canonical, p.canonical
		FROM resources AS r
		JOIN pipelines AS p
			ON r.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE r.next_check IS NOT NULL AND r.next_check <= ?
	`
	if r.system == PostgreSQL || r.system == MySQL {
		q += " FOR UPDATE SKIP LOCKED"
	}

	now := time.Now()
	rows, err := r.querier.QueryContext(ctx, q, now)
	if err != nil {
		return nil, fmt.Errorf("failed to filter due resources: %w", err)
	}

	var results []*resource.ResourceWithPipeline
	for rows.Next() {
		var (
			dbr dbResource
			tc  sql.NullString
			pn  sql.NullString
		)
		err := rows.Scan(
			&dbr.ID,
			&dbr.Name,
			&dbr.Type,
			&dbr.Canonical,
			&dbr.Params,
			&dbr.CheckInterval,
			&dbr.Logs,
			&dbr.LastCheck,
			&dbr.NextCheck,
			&dbr.WebhookToken,
			&dbr.Cache,
			&dbr.PinnedVersionID,
			&tc,
			&pn,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan due resource: %w", err)
		}
		results = append(results, &resource.ResourceWithPipeline{
			Resource:          *dbr.toDomainEntity(),
			TeamCanonical:     tc.String,
			PipelineCanonical: pn.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate due resources: %w", err)
	}
	return results, nil
}

func (r *ResourceRepository) CreateVersion(ctx context.Context, tc, pn, rCan string, rv resource.Version) (uint32, error) {
	dbrv := newDBResourceVersion(rv)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO resource_versions(version, resource_id)
		VALUES (?, 
			-- resource_id
			(
				SELECT r.id
				FROM resources AS r
				JOIN pipelines AS p
					ON r.pipeline_id = p.id
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?
			))`, dbrv.Version, tc, pn, rCan)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *ResourceRepository) FilterVersions(ctx context.Context, tc, pn, rCan string, before *uint32, after *uint32, limit uint32) ([]*resource.Version, error) {
	query := `
		SELECT rv.id, rv.version
		FROM resource_versions AS rv
		JOIN resources AS r
			ON rv.resource_id = r.id
		JOIN pipelines AS p
			ON r.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?`
	args := []interface{}{tc, pn, rCan}

	if after != nil {
		query += ` AND rv.id > ?`
		args = append(args, *after)
		query += ` ORDER BY rv.id ASC`
	} else if before != nil {
		query += ` AND rv.id < ?`
		args = append(args, *before)
		query += ` ORDER BY rv.id DESC`
		if limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, limit)
		}
	} else {
		// Initial load or limit=0 (all)
		query += ` ORDER BY rv.id DESC`
		if limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, limit)
		}
	}

	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Resources: %w", err)
	}

	rvs, err := scanResourceVersions(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter resource versions: %w", err)
	}

	return rvs, nil
}

func (r *ResourceRepository) PinVersion(ctx context.Context, tc, pn, rCan string, versionID uint32) error {
	res, err := r.querier.ExecContext(ctx, `
		UPDATE resources AS r
		SET pinned_version_id = ?
		FROM (
			SELECT r.id
			FROM resources AS r
			JOIN pipelines AS p
				ON r.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?
		) AS rr
		WHERE rr.id = r.id
	`, versionID, tc, pn, rCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to pin resource version: %w", err)
	}

	return nil
}

func (r *ResourceRepository) UnpinVersion(ctx context.Context, tc, pn, rCan string) error {
	res, err := r.querier.ExecContext(ctx, `
		UPDATE resources AS r
		SET pinned_version_id = NULL
		FROM (
			SELECT r.id
			FROM resources AS r
			JOIN pipelines AS p
				ON r.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?
		) AS rr
		WHERE rr.id = r.id
	`, tc, pn, rCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to unpin resource version: %w", err)
	}

	return nil
}

func (r *ResourceRepository) Delete(ctx context.Context, tc, pn, rCan string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM resources
		WHERE id IN (
			SELECT r.id
			FROM resources AS r
			JOIN pipelines AS p
				ON r.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND r.canonical = ?
		)
	`, tc, pn, rCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the resource: %w", err)
	}

	return nil
}

func scanResource(s sqlr.Scanner) (*resource.Resource, error) {
	var r dbResource

	err := s.Scan(
		&r.ID,
		&r.Name,
		&r.Type,
		&r.Canonical,
		&r.Params,
		&r.CheckInterval,
		&r.Logs,
		&r.LastCheck,
		&r.NextCheck,
		&r.WebhookToken,
		&r.Cache,
		&r.PinnedVersionID,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return r.toDomainEntity(), nil
}

func scanResources(rows *sql.Rows) ([]*resource.Resource, error) {
	var rs []*resource.Resource

	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}
		rs = append(rs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan resource: %w", err)
	}
	return rs, nil
}

func scanResourceVersion(s sqlr.Scanner) (*resource.Version, error) {
	var rv dbResourceVersion

	err := s.Scan(
		&rv.ID,
		&rv.Version,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return rv.toDomainEntity(), nil
}

func scanResourceVersions(rows *sql.Rows) ([]*resource.Version, error) {
	var rvs []*resource.Version

	for rows.Next() {
		rv, err := scanResourceVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource version: %w", err)
		}
		rvs = append(rvs, rv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan resource version: %w", err)
	}
	return rvs, nil
}
