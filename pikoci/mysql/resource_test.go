package mysql_test

import (
	"context"
	"testing"
	"time"

	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLatestVersionByResources(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lvbr-pipe', 'lvbr-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	// Create two resources
	res, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache) VALUES (?, 'repo', 'git', 'git.repo', '', 0)`, ppID)
	require.NoError(t, err)
	repoID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache) VALUES (?, 'image', 'docker', 'docker.image', '', 0)`, ppID)
	require.NoError(t, err)
	imageID, _ := res.LastInsertId()

	// Insert multiple versions for repo resource
	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"ref":"abc"}')`, repoID)
	require.NoError(t, err)
	_, _ = res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"ref":"def"}')`, repoID)
	require.NoError(t, err)
	repoLatestID, _ := res.LastInsertId()

	// Insert multiple versions for image resource
	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"tag":"1.0"}')`, imageID)
	require.NoError(t, err)
	_, _ = res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"tag":"2.0"}')`, imageID)
	require.NoError(t, err)
	_, _ = res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"tag":"3.0"}')`, imageID)
	require.NoError(t, err)
	imageLatestID, _ := res.LastInsertId()

	rr := mysql.NewResourceRepository(db, mysql.Mem)

	result, err := rr.LatestVersionByResources(ctx, "main", "lvbr-pipe")
	require.NoError(t, err)

	// Map should be keyed by resource canonical
	assert.Contains(t, result, "git.repo")
	assert.Contains(t, result, "docker.image")

	// Each entry should be the latest (highest ID) version
	repoVersion := result["git.repo"]
	require.NotNil(t, repoVersion)
	assert.Equal(t, uint32(repoLatestID), repoVersion.ID)
	assert.Equal(t, "def", repoVersion.Version["ref"])

	imageVersion := result["docker.image"]
	require.NotNil(t, imageVersion)
	assert.Equal(t, uint32(imageLatestID), imageVersion.ID)
	assert.Equal(t, "3.0", imageVersion.Version["tag"])

	t.Run("returns empty map when pipeline has no versions", func(t *testing.T) {
		res2, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lvbr-empty', 'lvbr-empty')`)
		require.NoError(t, err)
		emptyPPID, _ := res2.LastInsertId()
		// Insert a resource but no versions
		_, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache) VALUES (?, 'empty-res', 'git', 'git.empty', '', 0)`, emptyPPID)
		require.NoError(t, err)

		result, err := rr.LatestVersionByResources(ctx, "main", "lvbr-empty")
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("does not return versions from a different pipeline", func(t *testing.T) {
		result, err := rr.LatestVersionByResources(ctx, "main", "nonexistent-pipe")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestFindByWebhookToken(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'wh-pipe', 'wh-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx,
		`INSERT INTO resources (pipeline_id, name, type, canonical, webhook_token, tags, cache)
		 VALUES (?, 'repo', 'git', 'git.repo', 'git.repo_abc123', '["deploy"]', 0)`, ppID)
	require.NoError(t, err)

	rr := mysql.NewResourceRepository(db, mysql.Mem)

	r, tc, pc, err := rr.FindByWebhookToken(ctx, "git.repo_abc123")
	require.NoError(t, err)
	assert.Equal(t, "git.repo", r.Canonical)
	assert.Equal(t, "main", tc)
	assert.Equal(t, "wh-pipe", pc)
}

func TestFindByWebhookToken_NotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	rr := mysql.NewResourceRepository(db, mysql.Mem)

	_, _, _, err := rr.FindByWebhookToken(ctx, "nonexistent-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// UpdateLogs writes the check output and nothing else: the resource's type
// and params, which decide what a check runs, are what the pipeline said.
func TestUpdateLogs(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lg-pipe', 'lg-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx,
		`INSERT INTO resources (pipeline_id, name, type, canonical, params, logs, webhook_token, tags, cache)
		 VALUES (?, 'repo', 'git', 'git.repo', '{"params":{"uri":"git@example.com:app.git"}}', 'old', 'git.repo_tok', '[]', 0)`, ppID)
	require.NoError(t, err)

	rr := mysql.NewResourceRepository(db, mysql.Mem)

	require.NoError(t, rr.UpdateLogs(ctx, "main", "lg-pipe", "git.repo", "check failed: boom"))

	r, err := rr.Find(ctx, "main", "lg-pipe", "git.repo")
	require.NoError(t, err)
	assert.Equal(t, "check failed: boom", r.Logs)
	assert.Equal(t, "git", r.Type)
	assert.Equal(t, "git@example.com:app.git", r.GetParams()["uri"])

	err = rr.UpdateLogs(ctx, "main", "lg-pipe", "git.missing", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFilterDueResources_LongestWaitingFirst(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'due-pipe', 'due-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	// Inserted in id order, but the newest resource has waited longest -- the
	// shape a busy server reaches when a pipeline is added after many others.
	now := time.Now()
	for _, r := range []struct {
		name      string
		nextCheck time.Time
	}{
		{"old", now.Add(-1 * time.Minute)},
		{"mid", now.Add(-10 * time.Minute)},
		{"newest", now.Add(-30 * time.Minute)},
		{"future", now.Add(10 * time.Minute)}, // not due yet
	} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache, next_check) VALUES (?, ?, 'git', ?, '', 0, ?)`,
			ppID, r.name, "git."+r.name, r.nextCheck)
		require.NoError(t, err)
	}

	rr := mysql.NewResourceRepository(db, mysql.Mem)
	due, err := rr.FilterDueResources(ctx)
	require.NoError(t, err)

	var got []string
	for _, r := range due {
		// The in-memory database is shared across tests; ignore other pipelines.
		if r.PipelineCanonical == "due-pipe" {
			got = append(got, r.Canonical)
		}
	}
	// Oldest next_check first, so NextWork cannot starve the resources at the
	// end of the table when more fall due than the workers can check.
	assert.Equal(t, []string{"git.newest", "git.mid", "git.old"}, got)
}

func TestFilterDueResources_RequestedChecksFirst(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'req-pipe', 'req-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	now := time.Now()
	for _, r := range []struct {
		name      string
		nextCheck time.Time
	}{
		{"backlog-a", now.Add(-30 * time.Minute)},
		{"backlog-b", now.Add(-10 * time.Minute)},
		{"hooked", now.Add(10 * time.Minute)}, // not due until a webhook asks
	} {
		_, err := db.ExecContext(ctx,
			`INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache, next_check) VALUES (?, ?, 'git', ?, '', 0, ?)`,
			ppID, r.name, "git."+r.name, r.nextCheck)
		require.NoError(t, err)
	}

	rr := mysql.NewResourceRepository(db, mysql.Mem)
	require.NoError(t, rr.RequestCheck(ctx, "main", "req-pipe", "git.hooked", now))

	canonicals := func() []string {
		due, err := rr.FilterDueResources(ctx)
		require.NoError(t, err)
		var got []string
		for _, r := range due {
			// The in-memory database is shared across tests; ignore other pipelines.
			if r.PipelineCanonical == "req-pipe" {
				got = append(got, r.Canonical)
			}
		}
		return got
	}

	// The requested check jumps the overdue backlog even though its next_check
	// is the newest of the due resources.
	assert.Equal(t, []string{"git.hooked", "git.backlog-a", "git.backlog-b"}, canonicals())

	// Claiming clears the request, so the resource falls back to its schedule.
	claimed, err := rr.ClaimResourceCheck(ctx, "main", "req-pipe", "git.hooked", now, now, now.Add(-time.Second), 0)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, []string{"git.backlog-a", "git.backlog-b", "git.hooked"}, canonicals())
}

func TestRequestRetrigger_ClaimHandsVersionOutOnce(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'retrig-pipe', 'retrig-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()
	now := time.Now()
	_, err = db.ExecContext(ctx,
		`INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache, next_check) VALUES (?, 'repo', 'git', 'git.repo', '', 0, ?)`,
		ppID, now.Add(time.Hour))
	require.NoError(t, err)

	rr := mysql.NewResourceRepository(db, mysql.Mem)
	due := func() []uint32 {
		all, err := rr.FilterDueResources(ctx)
		require.NoError(t, err)
		var got []uint32
		for _, r := range all {
			// The in-memory database is shared across tests; ignore other pipelines.
			if r.PipelineCanonical == "retrig-pipe" {
				got = append(got, r.RetriggerVersionID)
			}
		}
		return got
	}
	require.Empty(t, due(), "not due before the re-trigger request")

	require.NoError(t, rr.RequestRetrigger(ctx, "main", "retrig-pipe", "git.repo", 7, now))
	assert.Equal(t, []uint32{7}, due(), "the request makes the resource due and carries the version")

	// A claim that read a different version (a newer click replaced it) loses
	// and leaves the request in place.
	claimed, err := rr.ClaimResourceCheck(ctx, "main", "retrig-pipe", "git.repo", now, now, now.Add(time.Hour), 3)
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Equal(t, []uint32{7}, due())

	claimed, err = rr.ClaimResourceCheck(ctx, "main", "retrig-pipe", "git.repo", now, now, now.Add(time.Hour), 7)
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Empty(t, due(), "the claim clears the request")

	// A second worker racing with the same read loses.
	claimed, err = rr.ClaimResourceCheck(ctx, "main", "retrig-pipe", "git.repo", now, now, now.Add(time.Hour), 7)
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestRequestRetrigger_RefusesSecondVersionWhilePending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'retrig2-pipe', 'retrig2-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()
	now := time.Now()
	_, err = db.ExecContext(ctx,
		`INSERT INTO resources (pipeline_id, name, type, canonical, tags, cache, next_check) VALUES (?, 'repo', 'git', 'git.repo', '', 0, ?)`,
		ppID, now.Add(time.Hour))
	require.NoError(t, err)

	rr := mysql.NewResourceRepository(db, mysql.Mem)
	require.NoError(t, rr.RequestRetrigger(ctx, "main", "retrig2-pipe", "git.repo", 10, now))

	// Same version again (double click): fine, nothing changes.
	require.NoError(t, rr.RequestRetrigger(ctx, "main", "retrig2-pipe", "git.repo", 10, now))

	// A different version while 10 waits would silently replace it: refused.
	err = rr.RequestRetrigger(ctx, "main", "retrig2-pipe", "git.repo", 12, now)
	require.ErrorIs(t, err, resource.ErrRetriggerPending)

	due, err := rr.FilterDueResources(ctx)
	require.NoError(t, err)
	for _, r := range due {
		if r.PipelineCanonical == "retrig2-pipe" {
			assert.Equal(t, uint32(10), r.RetriggerVersionID, "the first request is kept")
		}
	}

	// Once a worker has claimed 10, the next version is accepted.
	claimed, err := rr.ClaimResourceCheck(ctx, "main", "retrig2-pipe", "git.repo", now, now, now.Add(time.Hour), 10)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, rr.RequestRetrigger(ctx, "main", "retrig2-pipe", "git.repo", 12, now))
}

func TestRequestRetrigger_NotFound(t *testing.T) {
	db := setupTestDB(t)
	rr := mysql.NewResourceRepository(db, mysql.Mem)

	err := rr.RequestRetrigger(context.Background(), "main", "nope", "git.nope", 1, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRequestCheck_NotFound(t *testing.T) {
	db := setupTestDB(t)
	rr := mysql.NewResourceRepository(db, mysql.Mem)

	err := rr.RequestCheck(context.Background(), "main", "nope", "git.nope", time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
