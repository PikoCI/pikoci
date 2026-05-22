package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/xescugc/pikoci/pikoci/pipeline"
)

type PipelineRepository struct {
	querier sqlr.Querier
}

func NewPipelineRepository(db sqlr.Querier) *PipelineRepository {
	return &PipelineRepository{
		querier: db,
	}
}

type dbPipeline struct {
	ID        sql.NullInt64
	Name      sql.NullString
	Canonical sql.NullString
	Raw       sql.NullString
	Public    sql.NullBool
}

func newDBPipeline(p pipeline.Pipeline) dbPipeline {
	return dbPipeline{
		Name:      toNullString(p.Name),
		Canonical: toNullString(p.Canonical),
		Raw:       toNullString(string(p.Raw)),
		Public:    sql.NullBool{Bool: p.Public, Valid: true},
	}
}

func (dbp *dbPipeline) toDomainEntity() *pipeline.Pipeline {
	return &pipeline.Pipeline{
		ID:        uint32(dbp.ID.Int64),
		Name:      dbp.Name.String,
		Canonical: dbp.Canonical.String,
		Raw:       []byte(dbp.Raw.String),
		Public:    dbp.Public.Bool,
	}
}

func (r *PipelineRepository) Create(ctx context.Context, tc string, p pipeline.Pipeline) (uint32, error) {
	dbp := newDBPipeline(p)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO pipelines(name, canonical, raw, public, team_id)
		VALUES (?, ?, ?, ?,
			-- pipeline_id
			(
				SELECT t.id
				FROM teams AS t
				WHERE t.canonical = ?
			))`, dbp.Name, dbp.Canonical, dbp.Raw, dbp.Public, tc)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *PipelineRepository) Update(ctx context.Context, tc, pCan string, p pipeline.Pipeline) error {
	dbp := newDBPipeline(p)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE pipelines AS p
		SET name = ?, canonical = ?, raw = ?, public = ?
		FROM (
			SELECT p.id
			FROM pipelines AS p
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ?
		) AS pp
		WHERE p.id = pp.id
	`, dbp.Name, dbp.Canonical, dbp.Raw, dbp.Public, tc, pCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	return nil
}

func (r *PipelineRepository) Find(ctx context.Context, tc, pCan string) (*pipeline.Pipeline, error) {
	rows, err := r.querier.QueryContext(ctx, pipelineQuery+`
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to query Pipeline: %w", err)
	}

	pps, err := scanPipelines(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Pipeline: %w", err)
	}

	if len(pps) == 0 {
		return nil, fmt.Errorf("not found")
	}

	return pps[0], nil
}

func (r *PipelineRepository) FindPublic(ctx context.Context, tc, pCan string) (*pipeline.Pipeline, error) {
	rows, err := r.querier.QueryContext(ctx, pipelineQuery+`
		WHERE t.canonical = ? AND p.canonical = ? AND p.public = TRUE
	`, tc, pCan)
	if err != nil {
		return nil, fmt.Errorf("failed to query Pipeline: %w", err)
	}

	pps, err := scanPipelines(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Pipeline: %w", err)
	}

	if len(pps) == 0 {
		return nil, fmt.Errorf("not found")
	}

	return pps[0], nil
}

func (r *PipelineRepository) SetPublic(ctx context.Context, tc, pCan string, public bool) error {
	res, err := r.querier.ExecContext(ctx, `
		UPDATE pipelines AS p
		SET public = ?
		FROM (
			SELECT p.id
			FROM pipelines AS p
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ?
		) AS pp
		WHERE p.id = pp.id
	`, public, tc, pCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to set public on Pipeline: %w", err)
	}

	return nil
}

func (r *PipelineRepository) Filter(ctx context.Context, tc string) ([]*pipeline.Pipeline, error) {
	rows, err := r.querier.QueryContext(ctx, pipelineQuery+`
		WHERE t.canonical = ?
	`, tc)
	if err != nil {
		return nil, fmt.Errorf("failed to query Pipelines: %w", err)
	}

	pps, err := scanPipelines(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Pipelines: %w", err)
	}

	return pps, nil
}

func (r *PipelineRepository) FilterAll(ctx context.Context) ([]*pipeline.WithTeam, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT
			t.id, t.name, t.canonical,
			p.id, p.name, p.canonical, p.raw, p.public,
			j.id, j.name, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure,
			r.id, r.name, r.type, r.canonical, r.params, r.check_interval, r.logs, r.last_check, r.next_check,
			rt.id, rt.name, rt.`+"`check`"+`, rt.pull, rt.push, rt.params,
			ru.id, ru.name, ru.run,
			st.id, st.name, st.source, st.get, st.params, st.config
		FROM pipelines AS p
		JOIN teams AS t ON p.team_id = t.id
		LEFT JOIN jobs AS j ON j.pipeline_id = p.id
		LEFT JOIN resources AS r ON r.pipeline_id = p.id
		LEFT JOIN resource_types AS rt ON rt.pipeline_id = p.id
		LEFT JOIN runners AS ru ON ru.pipeline_id = p.id
		LEFT JOIN secret_types AS st ON st.pipeline_id = p.id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query all Pipelines: %w", err)
	}

	pps, err := scanPipelinesWithTeam(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan all Pipelines: %w", err)
	}

	return pps, nil
}

func scanPipelinesWithTeam(rows *sql.Rows) ([]*pipeline.WithTeam, error) {
	pipelineMap := make(map[uint32]*pipeline.WithTeam)
	var pipelineOrder []uint32

	type seenKey struct {
		pipelineID uint32
		table      string
		id         uint32
	}
	seen := make(map[seenKey]bool)

	for rows.Next() {
		var (
			tt dbTeam
			pp dbPipeline
			j  dbJob
			r  dbResource
			rt dbResourceType
			ru dbRunner
			st dbSecretType
		)

		err := rows.Scan(
			&tt.ID, &tt.Name, &tt.Canonical,
			&pp.ID, &pp.Name, &pp.Canonical, &pp.Raw, &pp.Public,
			&j.ID, &j.Name, &j.Plan, &j.OnSuccess, &j.OnFailure, &j.OnCancel, &j.Ensure,
			&r.ID, &r.Name, &r.Type, &r.Canonical, &r.Params, &r.CheckInterval, &r.Logs, &r.LastCheck, &r.NextCheck,
			&rt.ID, &rt.Name, &rt.Check, &rt.Pull, &rt.Push, &rt.Params,
			&ru.ID, &ru.Name, &ru.Run,
			&st.ID, &st.Name, &st.Source, &st.Get, &st.Params, &st.Config,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan: %w", err)
		}

		ppID := uint32(pp.ID.Int64)
		p, ok := pipelineMap[ppID]
		if !ok {
			p = &pipeline.WithTeam{
				Pipeline: *pp.toDomainEntity(),
				Team:     *tt.toDomainEntity(),
			}
			pipelineMap[ppID] = p
			pipelineOrder = append(pipelineOrder, ppID)
		}

		if j.ID.Valid {
			k := seenKey{ppID, "j", uint32(j.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.Jobs = append(p.Jobs, *j.toDomainEntity())
			}
		}
		if r.ID.Valid {
			k := seenKey{ppID, "r", uint32(r.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.Resources = append(p.Resources, *r.toDomainEntity())
			}
		}
		if rt.ID.Valid {
			k := seenKey{ppID, "rt", uint32(rt.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.ResourceTypes = append(p.ResourceTypes, *rt.toDomainEntity())
			}
		}
		if ru.ID.Valid {
			k := seenKey{ppID, "ru", uint32(ru.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.Runners = append(p.Runners, *ru.toDomainEntity())
			}
		}
		if st.ID.Valid {
			k := seenKey{ppID, "st", uint32(st.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.SecretTypes = append(p.SecretTypes, *st.toDomainEntity())
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	result := make([]*pipeline.WithTeam, 0, len(pipelineOrder))
	for _, id := range pipelineOrder {
		result = append(result, pipelineMap[id])
	}
	return result, nil
}

func (r *PipelineRepository) Delete(ctx context.Context, tc, pCan string) error {
	// Use a two-step delete: first resolve the ID, then delete by ID.
	// SQLite does not trigger ON DELETE CASCADE with subquery-based deletes.
	var id uint32
	err := r.querier.QueryRowContext(ctx, `
		SELECT p.id
		FROM pipelines AS p
		JOIN teams AS t ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pCan).Scan(&id)
	if err != nil {
		return fmt.Errorf("failed to find pipeline for deletion: %w", err)
	}

	res, err := r.querier.ExecContext(ctx, `DELETE FROM pipelines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the Pipeline: %w", err)
	}

	return nil
}

const pipelineQuery = `
	SELECT
		p.id, p.name, p.canonical, p.raw, p.public,
		j.id, j.name, j.plan, j.on_success, j.on_failure, j.on_cancel, j.ensure,
		r.id, r.name, r.type, r.canonical, r.params, r.check_interval, r.logs, r.last_check, r.next_check,
		rt.id, rt.name, rt.` + "`check`" + `, rt.pull, rt.push, rt.params,
		ru.id, ru.name, ru.run,
		st.id, st.name, st.source, st.get, st.params, st.config
	FROM pipelines AS p
	JOIN teams AS t ON p.team_id = t.id
	LEFT JOIN jobs AS j ON j.pipeline_id = p.id
	LEFT JOIN resources AS r ON r.pipeline_id = p.id
	LEFT JOIN resource_types AS rt ON rt.pipeline_id = p.id
	LEFT JOIN runners AS ru ON ru.pipeline_id = p.id
	LEFT JOIN secret_types AS st ON st.pipeline_id = p.id
`

func scanPipelines(rows *sql.Rows) ([]*pipeline.Pipeline, error) {
	pipelineMap := make(map[uint32]*pipeline.Pipeline)
	var pipelineOrder []uint32

	// Track seen IDs to avoid duplicates from the JOIN
	type seenKey struct {
		pipelineID uint32
		table      string
		id         uint32
	}
	seen := make(map[seenKey]bool)

	for rows.Next() {
		var (
			pp dbPipeline
			j  dbJob
			r  dbResource
			rt dbResourceType
			ru dbRunner
			st dbSecretType
		)

		err := rows.Scan(
			&pp.ID, &pp.Name, &pp.Canonical, &pp.Raw, &pp.Public,
			&j.ID, &j.Name, &j.Plan, &j.OnSuccess, &j.OnFailure, &j.OnCancel, &j.Ensure,
			&r.ID, &r.Name, &r.Type, &r.Canonical, &r.Params, &r.CheckInterval, &r.Logs, &r.LastCheck, &r.NextCheck,
			&rt.ID, &rt.Name, &rt.Check, &rt.Pull, &rt.Push, &rt.Params,
			&ru.ID, &ru.Name, &ru.Run,
			&st.ID, &st.Name, &st.Source, &st.Get, &st.Params, &st.Config,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan: %w", err)
		}

		ppID := uint32(pp.ID.Int64)
		p, ok := pipelineMap[ppID]
		if !ok {
			p = pp.toDomainEntity()
			pipelineMap[ppID] = p
			pipelineOrder = append(pipelineOrder, ppID)
		}

		if j.ID.Valid {
			k := seenKey{ppID, "j", uint32(j.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.Jobs = append(p.Jobs, *j.toDomainEntity())
			}
		}
		if r.ID.Valid {
			k := seenKey{ppID, "r", uint32(r.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.Resources = append(p.Resources, *r.toDomainEntity())
			}
		}
		if rt.ID.Valid {
			k := seenKey{ppID, "rt", uint32(rt.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.ResourceTypes = append(p.ResourceTypes, *rt.toDomainEntity())
			}
		}
		if ru.ID.Valid {
			k := seenKey{ppID, "ru", uint32(ru.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.Runners = append(p.Runners, *ru.toDomainEntity())
			}
		}
		if st.ID.Valid {
			k := seenKey{ppID, "st", uint32(st.ID.Int64)}
			if !seen[k] {
				seen[k] = true
				p.SecretTypes = append(p.SecretTypes, *st.toDomainEntity())
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	result := make([]*pipeline.Pipeline, 0, len(pipelineOrder))
	for _, id := range pipelineOrder {
		result = append(result, pipelineMap[id])
	}
	return result, nil
}
