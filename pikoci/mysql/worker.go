package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/pikoci/pikoci/pikoci/wkr"
)

type WorkerRepository struct {
	querier sqlr.Querier
	system  string
}

func NewWorkerRepository(db sqlr.Querier, system string) *WorkerRepository {
	return &WorkerRepository{
		querier: db,
		system:  system,
	}
}

func (r *WorkerRepository) Upsert(ctx context.Context, w wkr.Worker) error {
	tagsStr := strings.Join(w.Tags, ",")
	// "commit" is a reserved word in SQLite, so we quote it with backticks
	// (works in MySQL and SQLite; adapted to double-quotes for PostgreSQL
	// by the migrate package).
	var q string
	switch r.system {
	case MySQL:
		q = "INSERT INTO workers (name, hostname, os, arch, go_version, version, `commit`, concurrency, tags, exclusive_tags, team_canonical, started_at, last_ping_at)" +
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)" +
			" ON DUPLICATE KEY UPDATE" +
			" hostname = VALUES(hostname)," +
			" os = VALUES(os)," +
			" arch = VALUES(arch)," +
			" go_version = VALUES(go_version)," +
			" version = VALUES(version)," +
			" `commit` = VALUES(`commit`)," +
			" concurrency = VALUES(concurrency)," +
			" tags = VALUES(tags)," +
			" exclusive_tags = VALUES(exclusive_tags)," +
			" team_canonical = VALUES(team_canonical)," +
			" started_at = VALUES(started_at)," +
			" last_ping_at = VALUES(last_ping_at)"
	default:
		// SQLite, mem, PostgreSQL
		q = "INSERT INTO workers (name, hostname, os, arch, go_version, version, `commit`, concurrency, tags, exclusive_tags, team_canonical, started_at, last_ping_at)" +
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)" +
			" ON CONFLICT(name) DO UPDATE SET" +
			" hostname = excluded.hostname," +
			" os = excluded.os," +
			" arch = excluded.arch," +
			" go_version = excluded.go_version," +
			" version = excluded.version," +
			" `commit` = excluded.`commit`," +
			" concurrency = excluded.concurrency," +
			" tags = excluded.tags," +
			" exclusive_tags = excluded.exclusive_tags," +
			" team_canonical = excluded.team_canonical," +
			" started_at = excluded.started_at," +
			" last_ping_at = excluded.last_ping_at"
	}

	_, err := r.querier.ExecContext(ctx, q,
		w.Name, w.Hostname, w.OS, w.Arch, w.GoVersion, w.Version, w.Commit,
		w.Concurrency, tagsStr, w.ExclusiveTags, w.TeamCanonical, w.StartedAt, w.LastPingAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert worker: %w", err)
	}
	return nil
}

func (r *WorkerRepository) Filter(ctx context.Context) ([]*wkr.Worker, error) {
	rows, err := r.querier.QueryContext(ctx,
		"SELECT id, name, hostname, os, arch, go_version, version, `commit`, concurrency, tags, exclusive_tags, team_canonical, started_at, last_ping_at"+
			" FROM workers ORDER BY name ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to query workers: %w", err)
	}
	defer rows.Close()

	var workers []*wkr.Worker
	now := time.Now()
	for rows.Next() {
		var (
			id            sql.NullInt64
			name          sql.NullString
			hostname      sql.NullString
			os            sql.NullString
			arch          sql.NullString
			goVersion     sql.NullString
			version       sql.NullString
			commit        sql.NullString
			concurrency   sql.NullInt64
			tagsStr       sql.NullString
			exclusiveTags sql.NullBool
			teamCanonical sql.NullString
			startedAt     sql.NullTime
			lastPingAt    sql.NullTime
		)
		if err := rows.Scan(&id, &name, &hostname, &os, &arch, &goVersion, &version, &commit, &concurrency, &tagsStr, &exclusiveTags, &teamCanonical, &startedAt, &lastPingAt); err != nil {
			return nil, fmt.Errorf("failed to scan worker: %w", err)
		}
		var tags []string
		if tagsStr.String != "" {
			tags = strings.Split(tagsStr.String, ",")
		}
		w := &wkr.Worker{
			ID:            uint32(id.Int64),
			Name:          name.String,
			Hostname:      hostname.String,
			OS:            os.String,
			Arch:          arch.String,
			GoVersion:     goVersion.String,
			Version:       version.String,
			Commit:        commit.String,
			Concurrency:   int(concurrency.Int64),
			Tags:          tags,
			ExclusiveTags: exclusiveTags.Bool,
			TeamCanonical: teamCanonical.String,
			StartedAt:     startedAt.Time,
			LastPingAt:    lastPingAt.Time,
		}
		w.ComputeStatus(now)
		workers = append(workers, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return workers, nil
}

func (r *WorkerRepository) DeleteBefore(ctx context.Context, before time.Time) error {
	_, err := r.querier.ExecContext(ctx, `DELETE FROM workers WHERE last_ping_at < ?`, before)
	if err != nil {
		return fmt.Errorf("failed to delete old workers: %w", err)
	}
	return nil
}

func (r *WorkerRepository) DeleteByName(ctx context.Context, name string) error {
	res, err := r.querier.ExecContext(ctx, `DELETE FROM workers WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("failed to delete worker: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("worker %q not found", name)
	}
	return nil
}
