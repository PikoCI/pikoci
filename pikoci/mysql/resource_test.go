package mysql_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/mysql"
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
