package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/xescugc/pikoci/pikoci/trigger"
)

type TriggerRepository struct {
	querier sqlr.Querier
}

func NewTriggerRepository(db sqlr.Querier) *TriggerRepository {
	return &TriggerRepository{
		querier: db,
	}
}

func (r *TriggerRepository) Create(ctx context.Context, tc, name string, version map[string]interface{}) (*trigger.Trigger, error) {
	vb, err := json.Marshal(version)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal version: %w", err)
	}

	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO triggers(team_id, name, version)
		VALUES (
			(SELECT t.id FROM teams AS t WHERE t.canonical = ?),
			?, ?
		)
	`, tc, name, string(vb))
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return nil, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	t := &trigger.Trigger{
		ID:      id,
		Name:    name,
		Version: version,
	}

	return t, nil
}

func (r *TriggerRepository) FilterAfter(ctx context.Context, tc, name string, afterID uint32) ([]*trigger.Trigger, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT tr.id, tr.name, tr.version, tr.created_at
		FROM triggers AS tr
		JOIN teams AS t ON tr.team_id = t.id
		WHERE t.canonical = ?
			AND tr.name = ?
			AND tr.id > ?
		ORDER BY tr.id ASC
	`, tc, name, afterID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var triggers []*trigger.Trigger
	for rows.Next() {
		var (
			id        sql.NullInt64
			n         sql.NullString
			versionJS sql.NullString
			createdAt sql.NullTime
		)
		if err := rows.Scan(&id, &n, &versionJS, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		var version map[string]interface{}
		if versionJS.Valid {
			if err := json.Unmarshal([]byte(versionJS.String), &version); err != nil {
				return nil, fmt.Errorf("failed to unmarshal version: %w", err)
			}
		}
		triggers = append(triggers, &trigger.Trigger{
			ID:        uint32(id.Int64),
			Name:      n.String,
			Version:   version,
			CreatedAt: createdAt.Time,
		})
	}

	return triggers, nil
}
