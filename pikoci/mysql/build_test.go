package mysql_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/mysql"
)

func TestFind(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'find-pipe', 'find-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	now := time.Now().Round(0).UTC()
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, started_at, build_number) VALUES (?, 'started', ?, '1')`, jobID, now)
	require.NoError(t, err)
	buildID, _ := res.LastInsertId()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	b, err := br.Find(ctx, "main", "find-pipe", "build", "1")
	require.NoError(t, err)
	assert.Equal(t, uint32(buildID), b.ID)
	assert.Equal(t, "1", b.BuildNumber)
	assert.Equal(t, "started", b.Status.String())
}

func TestInsertGetVersion(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-insert', 'bgv-insert')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, jobID)
	require.NoError(t, err)
	buildID, _ := res.LastInsertId()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	err = br.InsertGetVersion(ctx, "main", "bgv-insert", "lint", uint32(buildID), "repo", 42)
	require.NoError(t, err)

	var versionID int
	err = db.QueryRowContext(ctx, `SELECT version_id FROM build_get_versions WHERE build_id = ? AND step_name = ?`, buildID, "repo").Scan(&versionID)
	require.NoError(t, err)
	assert.Equal(t, 42, versionID)

	// INSERT OR IGNORE: same (build_id, step_name) keeps original value
	err = br.InsertGetVersion(ctx, "main", "bgv-insert", "lint", uint32(buildID), "repo", 99)
	require.NoError(t, err)

	err = db.QueryRowContext(ctx, `SELECT version_id FROM build_get_versions WHERE build_id = ? AND step_name = ?`, buildID, "repo").Scan(&versionID)
	require.NoError(t, err)
	assert.Equal(t, 42, versionID)
}

func TestLastBuildAtByPipeline(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create two pipelines
	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lba-pipe-a', 'lba-pipe-a')`)
	require.NoError(t, err)
	ppAID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lba-pipe-b', 'lba-pipe-b')`)
	require.NoError(t, err)
	ppBID, _ := res.LastInsertId()

	// Pipeline A has two jobs with builds
	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppAID)
	require.NoError(t, err)
	jobAID, _ := res.LastInsertId()

	t1 := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 3, 2, 12, 0, 0, 0, time.UTC)

	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, started_at, build_number) VALUES (?, 'succeeded', ?, '1')`, jobAID, t1)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, started_at, build_number) VALUES (?, 'failed', ?, '2')`, jobAID, t2)
	require.NoError(t, err)

	// Pipeline B has no builds — only a job
	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppBID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)
	result, err := br.LastBuildAtByPipeline(ctx, "main")
	require.NoError(t, err)

	// Pipeline A should have the latest build time
	assert.Contains(t, result, uint32(ppAID))
	assert.Equal(t, t2, result[uint32(ppAID)])

	// Pipeline B should not be in the map (no builds)
	assert.NotContains(t, result, uint32(ppBID))
}

func TestLastBuildAtByPipeline_GoMonotonicFormat(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lba-mono', 'lba-mono')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	// Insert with Go's time.Time.String() format including monotonic clock suffix
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, started_at, build_number) VALUES (?, 'succeeded', ?, '1')`,
		jobID, "2026-05-20 11:16:14.81137605 +0000 UTC m=+630.364992545")
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)
	result, err := br.LastBuildAtByPipeline(ctx, "main")
	require.NoError(t, err)
	assert.Contains(t, result, uint32(ppID))
	assert.Equal(t, 2026, result[uint32(ppID)].Year())
	assert.Equal(t, time.May, result[uint32(ppID)].Month())
	assert.Equal(t, 20, result[uint32(ppID)].Day())
}

func TestLastBuildAtByPipeline_NoBuildsDifferentTeam(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create a pipeline under a different team
	res, err := db.ExecContext(ctx, `INSERT INTO teams (name, canonical) VALUES ('other-team', 'other')`)
	require.NoError(t, err)
	otherTeamID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (?, 'lba-other', 'lba-other')`, otherTeamID)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, started_at, build_number) VALUES (?, 'succeeded', ?, '1')`, jobID, time.Now())
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	// Querying for "main" team should return empty map
	result, err := br.LastBuildAtByPipeline(ctx, "main")
	require.NoError(t, err)
	assert.NotContains(t, result, uint32(ppID))
}

func TestFindReadyDownstreamVersion_BasicCase(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-basic', 'bgv-basic')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppID)
	require.NoError(t, err)
	testJobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)

	// Both upstream jobs succeeded with version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, lintJobID)
	require.NoError(t, err)
	lintBuildID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, testJobID)
	require.NoError(t, err)
	testBuildID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, lintBuildID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, testBuildID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	vID, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-basic",
		[]string{"lint", "test"}, "deploy", "repo", 2)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Equal(t, uint32(10), vID)
}

func TestFindReadyDownstreamVersion_NotAllUpstreamsReady(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-partial', 'bgv-partial')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)

	// Only lint succeeded with version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, lintJobID)
	require.NoError(t, err)
	lintBuildID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, lintBuildID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-partial",
		[]string{"lint", "test"}, "deploy", "repo", 2)
	require.NoError(t, err)
	assert.False(t, ready)
}

func TestFindReadyDownstreamVersion_AlreadyBuiltByDownstream(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-already', 'bgv-already')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)
	deployJobID, _ := res.LastInsertId()

	// Lint succeeded with version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, lintJobID)
	require.NoError(t, err)
	lintBuildID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, lintBuildID)
	require.NoError(t, err)

	// Deploy already consumed version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, deployJobID)
	require.NoError(t, err)
	deployBuildID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, deployBuildID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-already",
		[]string{"lint"}, "deploy", "repo", 1)
	require.NoError(t, err)
	assert.False(t, ready)
}

func TestFindReadyDownstreamVersion_FailedUpstreamIgnored(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-failed', 'bgv-failed')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)

	// Lint FAILED with version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'failed', '1')`, lintJobID)
	require.NoError(t, err)
	lintBuildID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, lintBuildID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-failed",
		[]string{"lint"}, "deploy", "repo", 1)
	require.NoError(t, err)
	assert.False(t, ready)
}

func TestFindReadyDownstreamVersion_MismatchedVersions(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-mismatch', 'bgv-mismatch')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppID)
	require.NoError(t, err)
	testJobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)

	// lint succeeded with version 10, test succeeded with version 12 — no common version
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, lintJobID)
	require.NoError(t, err)
	lintBuildID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, lintBuildID)
	require.NoError(t, err)

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, testJobID)
	require.NoError(t, err)
	testBuildID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 12)`, testBuildID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	// Should NOT be ready — lint has v10, test has v12, no common version
	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-mismatch",
		[]string{"lint", "test"}, "deploy", "repo", 2)
	require.NoError(t, err)
	assert.False(t, ready)
}

func TestFindReadyDownstreamVersion_PicksHighestVersion(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-highest', 'bgv-highest')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)

	// Lint succeeded with version 5
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, lintJobID)
	require.NoError(t, err)
	b1, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 5)`, b1)
	require.NoError(t, err)

	// Lint also succeeded with version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '2')`, lintJobID)
	require.NoError(t, err)
	b2, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, b2)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	vID, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-highest",
		[]string{"lint"}, "deploy", "repo", 1)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Equal(t, uint32(10), vID)
}
