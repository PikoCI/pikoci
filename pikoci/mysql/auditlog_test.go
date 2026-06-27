package mysql_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/auditlog"
	"github.com/pikoci/pikoci/pikoci/mysql"
)

func setupAuditLogTest(t *testing.T) (*sql.DB, *mysql.AuditLogRepository) {
	t.Helper()
	db := setupTestDB(t)
	// Clean audit_log table to isolate from other tests sharing the in-memory DB.
	_, err := db.ExecContext(context.Background(), `DELETE FROM audit_log`)
	require.NoError(t, err)
	return db, mysql.NewAuditLogRepository(db)
}

func createEntry(t *testing.T, repo *mysql.AuditLogRepository, team string, e auditlog.Entry) {
	t.Helper()
	err := repo.Create(context.Background(), team, e)
	require.NoError(t, err)
}

func TestAuditLogRepository_CreateAndFilter(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "pipe-a",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "bob", Action: auditlog.PipelineUpdated,
		TargetType: "pipeline", TargetName: "pipe-b",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "carol", Action: auditlog.JobTriggered,
		TargetType: "job", TargetName: "pipe-a/build",
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{})
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Newest first (ORDER BY id DESC)
	assert.Equal(t, "carol", entries[0].Actor)
	assert.Equal(t, "bob", entries[1].Actor)
	assert.Equal(t, "alice", entries[2].Actor)
}

func TestAuditLogRepository_FilterByActors(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "p1",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "bob", Action: auditlog.PipelineUpdated,
		TargetType: "pipeline", TargetName: "p2",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "carol", Action: auditlog.PipelineDeleted,
		TargetType: "pipeline", TargetName: "p3",
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		Actors: []string{"alice", "carol"},
	})
	require.NoError(t, err)
	require.Len(t, entries, 2)

	actors := []string{entries[0].Actor, entries[1].Actor}
	assert.Contains(t, actors, "alice")
	assert.Contains(t, actors, "carol")
}

func TestAuditLogRepository_FilterExcludeActors(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "p1",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "bot", Action: auditlog.PipelineUpdated,
		TargetType: "pipeline", TargetName: "p2",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "carol", Action: auditlog.PipelineDeleted,
		TargetType: "pipeline", TargetName: "p3",
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		ExcludeActors: []string{"bot"},
	})
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, e := range entries {
		assert.NotEqual(t, "bot", e.Actor)
	}
}

func TestAuditLogRepository_FilterByActions(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "p1",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineDeleted,
		TargetType: "pipeline", TargetName: "p2",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.JobTriggered,
		TargetType: "job", TargetName: "p1/build",
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		Actions: []auditlog.Action{auditlog.PipelineCreated, auditlog.PipelineDeleted},
	})
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, e := range entries {
		assert.Contains(t, []auditlog.Action{auditlog.PipelineCreated, auditlog.PipelineDeleted}, e.Action)
	}
}

func TestAuditLogRepository_FilterExcludeActions(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "p1",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.JobTriggered,
		TargetType: "job", TargetName: "p1/build",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.JobCancelled,
		TargetType: "job", TargetName: "p1/build",
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		ExcludeActions: []auditlog.Action{auditlog.JobTriggered, auditlog.JobCancelled},
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, auditlog.PipelineCreated, entries[0].Action)
}

func TestAuditLogRepository_FilterByPipelines(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "web-app",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.JobTriggered,
		TargetType: "job", TargetName: "web-app/build",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "api-server",
	})
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "unrelated",
	})

	// Filter by pipeline prefix "web-app" -- should match "web-app" and "web-app/build"
	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		Pipelines: []string{"web-app"},
	})
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, e := range entries {
		assert.Contains(t, e.TargetName, "web-app")
	}
}

func TestAuditLogRepository_FilterPagination(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	// Create 5 entries
	for i := 0; i < 5; i++ {
		createEntry(t, repo, "main", auditlog.Entry{
			Actor: "alice", Action: auditlog.PipelineCreated,
			TargetType: "pipeline", TargetName: "pipe",
		})
	}

	// Get all to know IDs
	all, err := repo.Filter(ctx, "main", auditlog.FilterOpts{})
	require.NoError(t, err)
	require.Len(t, all, 5)

	// Before cursor: entries with id < the 3rd entry's id (newest-first, so index 2)
	cursor := all[2].ID
	beforeEntries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		Before: &cursor,
	})
	require.NoError(t, err)
	require.Len(t, beforeEntries, 2)
	for _, e := range beforeEntries {
		assert.Less(t, e.ID, cursor)
	}

	// After cursor: entries with id > the 3rd entry's id
	afterEntries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		After: &cursor,
	})
	require.NoError(t, err)
	require.Len(t, afterEntries, 2)
	for _, e := range afterEntries {
		assert.Greater(t, e.ID, cursor)
	}
}

func TestAuditLogRepository_FilterLimit(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		createEntry(t, repo, "main", auditlog.Entry{
			Actor: "alice", Action: auditlog.PipelineCreated,
			TargetType: "pipeline", TargetName: "pipe",
		})
	}

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{
		Limit: 2,
	})
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestAuditLogRepository_CreateWithDetails(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	details := map[string]interface{}{
		"old_name": "alpha",
		"new_name": "beta",
	}
	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineUpdated,
		TargetType: "pipeline", TargetName: "pipe-x",
		Details: details,
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Details)
	assert.Equal(t, "alpha", entries[0].Details["old_name"])
	assert.Equal(t, "beta", entries[0].Details["new_name"])
}

func TestAuditLogRepository_CreateWithNilDetails(t *testing.T) {
	_, repo := setupAuditLogTest(t)
	ctx := context.Background()

	createEntry(t, repo, "main", auditlog.Entry{
		Actor: "alice", Action: auditlog.PipelineCreated,
		TargetType: "pipeline", TargetName: "pipe-nil",
		Details: nil,
	})

	entries, err := repo.Filter(ctx, "main", auditlog.FilterOpts{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Nil(t, entries[0].Details)
	assert.NotZero(t, entries[0].ID)
	assert.NotZero(t, entries[0].CreatedAt)
}
