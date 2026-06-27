package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/auditlog"
)

// AuditLogRepository implements auditlog.Repository using a SQL backend.
type AuditLogRepository struct {
	querier sqlr.Querier
}

// NewAuditLogRepository returns a new AuditLogRepository backed by the given querier.
func NewAuditLogRepository(db sqlr.Querier) *AuditLogRepository {
	return &AuditLogRepository{querier: db}
}

func (r *AuditLogRepository) Create(ctx context.Context, teamCanonical string, e auditlog.Entry) error {
	var detailsJSON *string
	if e.Details != nil {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("failed to marshal details: %w", err)
		}
		s := string(b)
		detailsJSON = &s
	}

	_, err := r.querier.ExecContext(ctx, `
		INSERT INTO audit_log (team_id, actor, action, target_type, target_name, details)
		VALUES (
			(SELECT t.id FROM teams AS t WHERE t.canonical = ?),
			?, ?, ?, ?, ?
		)
	`, teamCanonical, e.Actor, string(e.Action), e.TargetType, e.TargetName, detailsJSON)
	if err != nil {
		return fmt.Errorf("failed to insert audit log entry: %w", err)
	}
	return nil
}

// placeholders returns "?, ?, ?" for n items and appends vals to args.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (r *AuditLogRepository) Filter(ctx context.Context, teamCanonical string, opts auditlog.FilterOpts) ([]*auditlog.Entry, error) {
	var (
		conditions []string
		args       []interface{}
	)

	conditions = append(conditions, "t.canonical = ?")
	args = append(args, teamCanonical)

	if len(opts.Actors) > 0 {
		conditions = append(conditions, fmt.Sprintf("al.actor IN (%s)", placeholders(len(opts.Actors))))
		for _, a := range opts.Actors {
			args = append(args, a)
		}
	}
	if len(opts.ExcludeActors) > 0 {
		conditions = append(conditions, fmt.Sprintf("al.actor NOT IN (%s)", placeholders(len(opts.ExcludeActors))))
		for _, a := range opts.ExcludeActors {
			args = append(args, a)
		}
	}
	if len(opts.Actions) > 0 {
		conditions = append(conditions, fmt.Sprintf("al.action IN (%s)", placeholders(len(opts.Actions))))
		for _, a := range opts.Actions {
			args = append(args, string(a))
		}
	}
	if len(opts.ExcludeActions) > 0 {
		conditions = append(conditions, fmt.Sprintf("al.action NOT IN (%s)", placeholders(len(opts.ExcludeActions))))
		for _, a := range opts.ExcludeActions {
			args = append(args, string(a))
		}
	}
	if len(opts.Pipelines) > 0 {
		// OR across pipelines: (target_name LIKE 'a%' OR target_name LIKE 'b%')
		var pConds []string
		for _, p := range opts.Pipelines {
			pConds = append(pConds, "al.target_name LIKE ?")
			args = append(args, p+"%")
		}
		conditions = append(conditions, "("+strings.Join(pConds, " OR ")+")")
	}
	if opts.Since != nil {
		conditions = append(conditions, "al.created_at >= ?")
		args = append(args, *opts.Since)
	}
	if opts.Until != nil {
		conditions = append(conditions, "al.created_at <= ?")
		args = append(args, *opts.Until)
	}
	if opts.Before != nil {
		conditions = append(conditions, "al.id < ?")
		args = append(args, *opts.Before)
	}
	if opts.After != nil {
		conditions = append(conditions, "al.id > ?")
		args = append(args, *opts.After)
	}

	where := strings.Join(conditions, " AND ")

	limit := opts.Limit
	if limit == 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT al.id, al.actor, al.action, al.target_type, al.target_name, al.details, al.created_at
		FROM audit_log AS al
		JOIN teams AS t ON al.team_id = t.id
		WHERE %s
		ORDER BY al.id DESC
		LIMIT %d
	`, where, limit)

	rows, err := r.querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit log: %w", err)
	}
	defer rows.Close()

	var entries []*auditlog.Entry
	for rows.Next() {
		var (
			id         sql.NullInt64
			actor      sql.NullString
			action     sql.NullString
			targetType sql.NullString
			targetName sql.NullString
			detailsJS  sql.NullString
			createdAt  sql.NullTime
		)
		if err := rows.Scan(&id, &actor, &action, &targetType, &targetName, &detailsJS, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan audit log row: %w", err)
		}
		var details map[string]interface{}
		if detailsJS.Valid {
			if err := json.Unmarshal([]byte(detailsJS.String), &details); err != nil {
				return nil, fmt.Errorf("failed to unmarshal details: %w", err)
			}
		}
		entries = append(entries, &auditlog.Entry{
			ID:         uint32(id.Int64),
			Actor:      actor.String,
			Action:     auditlog.Action(action.String),
			TargetType: targetType.String,
			TargetName: targetName.String,
			Details:    details,
			CreatedAt:  createdAt.Time,
		})
	}

	return entries, nil
}
