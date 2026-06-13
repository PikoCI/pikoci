package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/notiftype"
)

type NotificationTypeRepository struct {
	querier sqlr.Querier
}

func NewNotificationTypeRepository(db sqlr.Querier) *NotificationTypeRepository {
	return &NotificationTypeRepository{
		querier: db,
	}
}

type dbNotificationType struct {
	ID     sql.NullInt64
	Name   sql.NullString
	Source sql.NullString
	Notify sql.NullString
	Params sql.NullString
	Runner sql.NullString
}

func newDBNotificationType(nt notiftype.NotificationType) dbNotificationType {
	p, _ := json.Marshal(nt.Params)
	n, _ := json.Marshal(nt.Notify)
	dbnt := dbNotificationType{
		Name:   toNullString(nt.Name),
		Source: toNullString(nt.Source),
		Notify: toNullString(string(n)),
		Params: toNullString(string(p)),
	}
	if nt.Runner != nil {
		r, _ := json.Marshal(nt.Runner)
		dbnt.Runner = toNullString(string(r))
	}
	return dbnt
}

func (dbnt *dbNotificationType) toDomainEntity() *notiftype.NotificationType {
	nt := &notiftype.NotificationType{
		ID:     uint32(dbnt.ID.Int64),
		Name:   dbnt.Name.String,
		Source: dbnt.Source.String,
	}

	_ = json.Unmarshal([]byte(dbnt.Params.String), &nt.Params)
	_ = json.Unmarshal([]byte(dbnt.Notify.String), &nt.Notify)
	if dbnt.Runner.Valid {
		_ = json.Unmarshal([]byte(dbnt.Runner.String), &nt.Runner)
	}

	return nt
}

func (r *NotificationTypeRepository) Create(ctx context.Context, tc, pn string, nt notiftype.NotificationType) (uint32, error) {
	dbnt := newDBNotificationType(nt)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO notification_types(name, source, notify, params, runner, pipeline_id)
		VALUES (?, ?, ?, ?, ?,
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbnt.Name, dbnt.Source, dbnt.Notify, dbnt.Params, dbnt.Runner, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *NotificationTypeRepository) Update(ctx context.Context, tc, pn, tn string, nt notiftype.NotificationType) error {
	dbnt := newDBNotificationType(nt)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE notification_types AS nt
		SET name = ?, source = ?, notify = ?, params = ?, runner = ?
		FROM (
			SELECT nt.id
			FROM notification_types AS nt
			JOIN pipelines AS p
				ON nt.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND nt.name = ?
		) AS ntt
		WHERE ntt.id = nt.id
	`, dbnt.Name, dbnt.Source, dbnt.Notify, dbnt.Params, dbnt.Runner, tc, pn, tn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update notification type: %w", err)
	}

	return nil
}

func (r *NotificationTypeRepository) Find(ctx context.Context, tc, pn, tn string) (*notiftype.NotificationType, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT nt.id, nt.name, nt.source, nt.notify, nt.params, nt.runner
		FROM notification_types AS nt
		JOIN pipelines AS p
			ON nt.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND nt.name = ?
	`, tc, pn, tn)

	nt, err := scanNotificationType(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan NotificationType: %w", err)
	}

	return nt, nil
}

func (r *NotificationTypeRepository) Filter(ctx context.Context, tc, pn string) ([]*notiftype.NotificationType, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT nt.id, nt.name, nt.source, nt.notify, nt.params, nt.runner
		FROM notification_types AS nt
		JOIN pipelines AS p
			ON nt.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter NotificationTypes: %w", err)
	}

	nts, err := scanNotificationTypes(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter notification types: %w", err)
	}

	return nts, nil
}

func (r *NotificationTypeRepository) Delete(ctx context.Context, tc, pn, tn string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM notification_types
		WHERE id IN (
			SELECT nt.id
			FROM notification_types AS nt
			JOIN pipelines AS p
				ON nt.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND nt.name = ?
		)
	`, tc, pn, tn)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the notification type: %w", err)
	}

	return nil
}

func scanNotificationType(s sqlr.Scanner) (*notiftype.NotificationType, error) {
	var nt dbNotificationType

	err := s.Scan(
		&nt.ID,
		&nt.Name,
		&nt.Source,
		&nt.Notify,
		&nt.Params,
		&nt.Runner,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return nt.toDomainEntity(), nil
}

func scanNotificationTypes(rows *sql.Rows) ([]*notiftype.NotificationType, error) {
	var nts []*notiftype.NotificationType

	for rows.Next() {
		nt, err := scanNotificationType(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification type: %w", err)
		}
		nts = append(nts, nt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan notification type: %w", err)
	}
	return nts, nil
}
