package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/restype"
)

type ResourceTypeRepository struct {
	querier sqlr.Querier
}

func NewResourceTypeRepository(db sqlr.Querier) *ResourceTypeRepository {
	return &ResourceTypeRepository{
		querier: db,
	}
}

type dbResourceType struct {
	ID     sql.NullInt64
	Name   sql.NullString
	Source sql.NullString
	Params sql.NullString
	Check  sql.NullString
	Pull   sql.NullString
	Push   sql.NullString
	Cache  sql.NullBool
	Runner sql.NullString
}

func newDBResourceType(rt restype.ResourceType) dbResourceType {
	i, _ := json.Marshal(rt.Params)
	c, _ := json.Marshal(rt.Check)
	pl, _ := json.Marshal(rt.Pull)
	ps, _ := json.Marshal(rt.Push)
	dbrt := dbResourceType{
		Name:   toNullString(rt.Name),
		Source: toNullString(rt.Source),
		Params: toNullString(string(i)),
		Check:  toNullString(string(c)),
		Pull:   toNullString(string(pl)),
		Push:   toNullString(string(ps)),
		Cache:  sql.NullBool{Bool: rt.Cache, Valid: true},
	}
	if rt.Runner != nil {
		r, _ := json.Marshal(rt.Runner)
		dbrt.Runner = toNullString(string(r))
	}
	return dbrt
}

func (dbrt *dbResourceType) toDomainEntity() *restype.ResourceType {
	rt := &restype.ResourceType{
		ID:     uint32(dbrt.ID.Int64),
		Name:   dbrt.Name.String,
		Source: dbrt.Source.String,
		Cache:  dbrt.Cache.Bool,
	}

	_ = json.Unmarshal([]byte(dbrt.Params.String), &rt.Params)
	_ = json.Unmarshal([]byte(dbrt.Check.String), &rt.Check)
	_ = json.Unmarshal([]byte(dbrt.Pull.String), &rt.Pull)
	_ = json.Unmarshal([]byte(dbrt.Push.String), &rt.Push)
	if dbrt.Runner.Valid {
		_ = json.Unmarshal([]byte(dbrt.Runner.String), &rt.Runner)
	}

	return rt
}

func (r *ResourceTypeRepository) Create(ctx context.Context, tc, pn string, rt restype.ResourceType) (uint32, error) {
	dbrt := newDBResourceType(rt)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO resource_types(name, source, `+"`check`"+`, pull, push, params, cache, runner, pipeline_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbrt.Name, dbrt.Source, dbrt.Check, dbrt.Pull, dbrt.Push, dbrt.Params, dbrt.Cache, dbrt.Runner, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *ResourceTypeRepository) Update(ctx context.Context, tc, pn, rtn string, rt restype.ResourceType) error {
	dbrt := newDBResourceType(rt)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE resource_types AS rt
		SET name = ?, source = ?, `+"`check`"+` = ?, pull = ?, push = ?, params = ?, cache = ?, runner = ?
		FROM (
			SELECT rt.id
			FROM resource_types AS rt
			JOIN pipelines AS p
				ON rt.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND rt.name = ?
		) AS rtt
		WHERE rtt.id = rt.id
	`, dbrt.Name, dbrt.Source, dbrt.Check, dbrt.Pull, dbrt.Push, dbrt.Params, dbrt.Cache, dbrt.Runner, tc, pn, rtn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update resource type: %w", err)
	}

	return nil
}

func (r *ResourceTypeRepository) Find(ctx context.Context, tc, pn, rtn string) (*restype.ResourceType, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT rt.id, rt.name, rt.source, `+"rt.`check`"+`, rt.pull, rt.push, rt.params, rt.cache, rt.runner
		FROM resource_types AS rt
		JOIN pipelines AS p
			ON rt.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND rt.name = ?
	`, tc, pn, rtn)

	rt, err := scanResourceType(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan ResourceType: %w", err)
	}

	return rt, nil
}

func (r *ResourceTypeRepository) Filter(ctx context.Context, tc, pn string) ([]*restype.ResourceType, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT rt.id, rt.name, rt.source, `+"rt.`check`"+`, rt.pull, rt.push, rt.params, rt.cache, rt.runner
		FROM resource_types AS rt
		JOIN pipelines AS p
			ON rt.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter ResourceTypes: %w", err)
	}

	restypes, err := scanResourceTypes(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter jobs: %w", err)
	}

	return restypes, nil
}

func (r *ResourceTypeRepository) Delete(ctx context.Context, tc, pn, rtn string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM resource_types
		WHERE id IN (
			SELECT rt.id
			FROM resource_types AS rt
			JOIN pipelines AS p
				ON rt.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND rt.name = ?
		)
	`, tc, pn, rtn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Job: %w", err)
	}

	return nil
}

func scanResourceType(s sqlr.Scanner) (*restype.ResourceType, error) {
	var rt dbResourceType

	err := s.Scan(
		&rt.ID,
		&rt.Name,
		&rt.Source,
		&rt.Check,
		&rt.Pull,
		&rt.Push,
		&rt.Params,
		&rt.Cache,
		&rt.Runner,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return rt.toDomainEntity(), nil
}

func scanResourceTypes(rows *sql.Rows) ([]*restype.ResourceType, error) {
	var rts []*restype.ResourceType

	for rows.Next() {
		rt, err := scanResourceType(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}
		rts = append(rts, rt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan resource: %w", err)
	}
	return rts, nil
}
