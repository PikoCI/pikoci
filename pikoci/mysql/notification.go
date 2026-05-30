package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/notification"
)

type NotificationRepository struct {
	querier sqlr.Querier
}

func NewNotificationRepository(db sqlr.Querier) *NotificationRepository {
	return &NotificationRepository{
		querier: db,
	}
}

type dbNotification struct {
	ID          sql.NullInt64
	Type        sql.NullString
	Name        sql.NullString
	Canonical   sql.NullString
	Params      sql.NullString
	Message     sql.NullString
	OnEvents    sql.NullString
	Jobs        sql.NullString
	ExcludeJobs sql.NullString
}

func newDBNotification(n notification.Notification) dbNotification {
	p, _ := json.Marshal(n.Params)
	on, _ := json.Marshal(n.On)
	jobs, _ := json.Marshal(n.Jobs)
	excl, _ := json.Marshal(n.Exclude)
	return dbNotification{
		Type:        toNullString(n.Type),
		Name:        toNullString(n.Name),
		Canonical:   toNullString(n.Canonical),
		Params:      toNullString(string(p)),
		Message:     toNullString(n.Message),
		OnEvents:    toNullString(string(on)),
		Jobs:        toNullString(string(jobs)),
		ExcludeJobs: toNullString(string(excl)),
	}
}

func (dbn *dbNotification) toDomainEntity() *notification.Notification {
	n := &notification.Notification{
		ID:        uint32(dbn.ID.Int64),
		Type:      dbn.Type.String,
		Name:      dbn.Name.String,
		Canonical: dbn.Canonical.String,
		Message:   dbn.Message.String,
	}

	_ = json.Unmarshal([]byte(dbn.Params.String), &n.Params)
	_ = json.Unmarshal([]byte(dbn.OnEvents.String), &n.On)
	_ = json.Unmarshal([]byte(dbn.Jobs.String), &n.Jobs)
	_ = json.Unmarshal([]byte(dbn.ExcludeJobs.String), &n.Exclude)

	return n
}

func (r *NotificationRepository) Create(ctx context.Context, tc, pn string, n notification.Notification) (uint32, error) {
	dbn := newDBNotification(n)
	res, err := r.querier.ExecContext(ctx, `
		INSERT INTO notifications(type, name, canonical, params, message, on_events, jobs, exclude_jobs, pipeline_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?,
			(
				SELECT p.id
				FROM pipelines AS p
				JOIN teams AS t
					ON p.team_id = t.id
				WHERE t.canonical = ? AND p.canonical = ?
			))`, dbn.Type, dbn.Name, dbn.Canonical, dbn.Params, dbn.Message, dbn.OnEvents, dbn.Jobs, dbn.ExcludeJobs, tc, pn)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	id, err := lastInsertedID(res)
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	return id, nil
}

func (r *NotificationRepository) Update(ctx context.Context, tc, pn, nCan string, n notification.Notification) error {
	dbn := newDBNotification(n)
	res, err := r.querier.ExecContext(ctx, `
		UPDATE notifications AS n
		SET type = ?, name = ?, canonical = ?, params = ?, message = ?, on_events = ?, jobs = ?, exclude_jobs = ?
		FROM (
			SELECT n.id
			FROM notifications AS n
			JOIN pipelines AS p
				ON n.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND n.canonical = ?
		) AS nn
		WHERE nn.id = n.id
	`, dbn.Type, dbn.Name, dbn.Canonical, dbn.Params, dbn.Message, dbn.OnEvents, dbn.Jobs, dbn.ExcludeJobs, tc, pn, nCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to update notification: %w", err)
	}

	return nil
}

func (r *NotificationRepository) Find(ctx context.Context, tc, pn, nCan string) (*notification.Notification, error) {
	row := r.querier.QueryRowContext(ctx, `
		SELECT n.id, n.type, n.name, n.canonical, n.params, n.message, n.on_events, n.jobs, n.exclude_jobs
		FROM notifications AS n
		JOIN pipelines AS p
			ON n.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ? AND n.canonical = ?
	`, tc, pn, nCan)

	n, err := scanNotification(row)
	if err != nil {
		return nil, fmt.Errorf("failed to scan Notification: %w", err)
	}

	return n, nil
}

func (r *NotificationRepository) Filter(ctx context.Context, tc, pn string) ([]*notification.Notification, error) {
	rows, err := r.querier.QueryContext(ctx, `
		SELECT n.id, n.type, n.name, n.canonical, n.params, n.message, n.on_events, n.jobs, n.exclude_jobs
		FROM notifications AS n
		JOIN pipelines AS p
			ON n.pipeline_id = p.id
		JOIN teams AS t
			ON p.team_id = t.id
		WHERE t.canonical = ? AND p.canonical = ?
	`, tc, pn)
	if err != nil {
		return nil, fmt.Errorf("failed to filter Notifications: %w", err)
	}

	ns, err := scanNotifications(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to filter notifications: %w", err)
	}

	return ns, nil
}

func (r *NotificationRepository) Delete(ctx context.Context, tc, pn, nCan string) error {
	res, err := r.querier.ExecContext(ctx, `
		DELETE
		FROM notifications
		WHERE id IN (
			SELECT n.id
			FROM notifications AS n
			JOIN pipelines AS p
				ON n.pipeline_id = p.id
			JOIN teams AS t
				ON p.team_id = t.id
			WHERE t.canonical = ? AND p.canonical = ? AND n.canonical = ?
		)
	`, tc, pn, nCan)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	err = isEntityFound(res)
	if err != nil {
		return fmt.Errorf("failed to delete the notification: %w", err)
	}

	return nil
}

func scanNotification(s sqlr.Scanner) (*notification.Notification, error) {
	var n dbNotification

	err := s.Scan(
		&n.ID,
		&n.Type,
		&n.Name,
		&n.Canonical,
		&n.Params,
		&n.Message,
		&n.OnEvents,
		&n.Jobs,
		&n.ExcludeJobs,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("not found")
		}
		return nil, fmt.Errorf("failed to scan: %w", err)
	}

	return n.toDomainEntity(), nil
}

func scanNotifications(rows *sql.Rows) ([]*notification.Notification, error) {
	var ns []*notification.Notification

	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", err)
		}
		ns = append(ns, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan notification: %w", err)
	}
	return ns, nil
}
