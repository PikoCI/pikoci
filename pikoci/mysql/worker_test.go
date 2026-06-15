package mysql_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/wkr"
)

func TestWorkerRepository_UpsertAndFilter_Commit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewWorkerRepository(db, mysql.Mem)

	now := time.Now().Truncate(time.Second)
	w := wkr.Worker{
		Name:        "test-worker",
		Hostname:    "host1",
		OS:          "linux",
		Arch:        "amd64",
		GoVersion:   "go1.22",
		Version:     "v0.5.0",
		Commit:      "abc1234",
		Concurrency: 2,
		StartedAt:   now,
		LastPingAt:  now,
	}

	err := repo.Upsert(ctx, w)
	require.NoError(t, err)

	workers, err := repo.Filter(ctx)
	require.NoError(t, err)
	require.Len(t, workers, 1)

	assert.Equal(t, "test-worker", workers[0].Name)
	assert.Equal(t, "v0.5.0", workers[0].Version)
	assert.Equal(t, "abc1234", workers[0].Commit)

	// Update commit via upsert
	w.Commit = "def5678"
	w.LastPingAt = now.Add(time.Second)
	err = repo.Upsert(ctx, w)
	require.NoError(t, err)

	workers, err = repo.Filter(ctx)
	require.NoError(t, err)
	require.Len(t, workers, 1)
	assert.Equal(t, "def5678", workers[0].Commit)
}

func TestWorkerRepository_UpsertAndFilter_EmptyCommit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewWorkerRepository(db, mysql.Mem)

	now := time.Now().Truncate(time.Second)
	w := wkr.Worker{
		Name:       "empty-commit-worker",
		Hostname:   "host2",
		Version:    "v0.4.0",
		Commit:     "",
		StartedAt:  now,
		LastPingAt: now,
	}

	err := repo.Upsert(ctx, w)
	require.NoError(t, err)

	workers, err := repo.Filter(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, workers)

	// Find our worker
	var found *wkr.Worker
	for _, wk := range workers {
		if wk.Name == "empty-commit-worker" {
			found = wk
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "", found.Commit)
}
