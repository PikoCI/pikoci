package mysql_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mysql"
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
		[]string{"lint", "test"}, "deploy", "repo", 2, nil)
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
		[]string{"lint", "test"}, "deploy", "repo", 2, nil)
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
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number, version_id) VALUES (?, 'succeeded', '1', 10)`, deployJobID)
	require.NoError(t, err)
	deployBuildID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, deployBuildID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-already",
		[]string{"lint"}, "deploy", "repo", 1, nil)
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
		[]string{"lint"}, "deploy", "repo", 1, nil)
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
		[]string{"lint", "test"}, "deploy", "repo", 2, nil)
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
		[]string{"lint"}, "deploy", "repo", 1, nil)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Equal(t, uint32(10), vID)
}

func TestFindReadyDownstreamVersion_BaselineFiltersOldVersions(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-baseline', 'bgv-baseline')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	lintJobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)

	// Lint succeeded with version 5 (before baseline)
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, lintJobID)
	require.NoError(t, err)
	b1, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 5)`, b1)
	require.NoError(t, err)

	// Lint succeeded with version 10 (after baseline)
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '2')`, lintJobID)
	require.NoError(t, err)
	b2, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, b2)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	// With baseline=7, only version 10 should be considered (>7), not version 5
	baseline := uint32(7)
	vID, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-baseline",
		[]string{"lint"}, "deploy", "repo", 1, &baseline)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Equal(t, uint32(10), vID)

	// With baseline=10, no versions should match (nothing > 10)
	baseline2 := uint32(10)
	_, ready, err = br.FindReadyDownstreamVersion(ctx, "main", "bgv-baseline",
		[]string{"lint"}, "deploy", "repo", 1, &baseline2)
	require.NoError(t, err)
	assert.False(t, ready)

	// With nil baseline, both versions are candidates — picks highest (10)
	vID, ready, err = br.FindReadyDownstreamVersion(ctx, "main", "bgv-baseline",
		[]string{"lint"}, "deploy", "repo", 1, nil)
	require.NoError(t, err)
	assert.True(t, ready)
	assert.Equal(t, uint32(10), vID)
}

func TestFindReadyDownstreamVersion_RenamedJobSkipsOldVersions(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Setup: pipeline with upstream "gen" and downstream "deploy-staging"
	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bgv-rename', 'bgv-rename')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	// Create a resource so resource_versions has entries
	res, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, canonical, type) VALUES (?, 'output', 'artifact.output', 'artifact')`, ppID)
	require.NoError(t, err)
	resourceID, _ := res.LastInsertId()

	// Insert old resource versions — capture actual IDs
	versionIDs := make([]int64, 3)
	for i := 0; i < 3; i++ {
		res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, ?)`,
			resourceID, fmt.Sprintf(`{"v":"%d"}`, i+1))
		require.NoError(t, err)
		versionIDs[i], _ = res.LastInsertId()
	}

	// Create upstream "gen" job with old succeeded builds
	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'gen')`, ppID)
	require.NoError(t, err)
	genJobID, _ := res.LastInsertId()

	// gen succeeded with each version
	for i := 0; i < 3; i++ {
		res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', ?)`, genJobID, i+1)
		require.NoError(t, err)
		buildID, _ := res.LastInsertId()
		_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'output', ?)`, buildID, versionIDs[i])
		require.NoError(t, err)
	}

	// Simulate renaming: create a new job using JobRepository.Create
	// which should set baseline_version_id = MAX(resource_versions.id)
	jr := mysql.NewJobRepository(db)
	jobID, err := jr.Create(ctx, "main", "bgv-rename", job.Job{Name: "deploy-staging-v2"})
	require.NoError(t, err)

	// Verify baseline was set to the last version ID
	newJob, err := jr.Find(ctx, "main", "bgv-rename", "deploy-staging-v2")
	require.NoError(t, err)
	require.NotNil(t, newJob.BaselineVersionID, "baseline_version_id should be set on new job")
	assert.Equal(t, uint32(versionIDs[2]), *newJob.BaselineVersionID)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	// With the baseline, all old versions should be filtered out
	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-rename",
		[]string{"gen"}, "deploy-staging-v2", "output", 1, newJob.BaselineVersionID)
	require.NoError(t, err)
	assert.False(t, ready, "old versions should be filtered by baseline")

	// Without baseline (nil), latest version would be returned
	vID, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "bgv-rename",
		[]string{"gen"}, "deploy-staging-v2", "output", 1, nil)
	require.NoError(t, err)
	assert.True(t, ready, "without baseline, old versions should be visible")
	assert.Equal(t, uint32(versionIDs[2]), vID)

	// Now add a NEW version after the baseline
	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"v":"4"}')`, resourceID)
	require.NoError(t, err)
	newVersionID, _ := res.LastInsertId()
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '4')`, genJobID)
	require.NoError(t, err)
	b4ID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'output', ?)`, b4ID, newVersionID)
	require.NoError(t, err)

	// With the baseline, only the new version should be returned
	vID, ready, err = br.FindReadyDownstreamVersion(ctx, "main", "bgv-rename",
		[]string{"gen"}, "deploy-staging-v2", "output", 1, newJob.BaselineVersionID)
	require.NoError(t, err)
	assert.True(t, ready, "version after baseline should be visible")
	assert.Equal(t, uint32(newVersionID), vID)

	_ = jobID
}

// TestFindReadyDownstreamVersion_CronPipelineRenameScenario mirrors the cron.hcl pipeline:
// gen (GET cron + PUT artifact) → deploy-staging (GET artifact, passed=["gen"])
// and also a monitor job (GET cron, passed=["gen"]).
// When deploy-staging or monitor is renamed, old versions must not replay.
func TestFindReadyDownstreamVersion_CronPipelineRenameScenario(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Setup pipeline
	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'cron-rename', 'cron-rename')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	// Create cron resource + versions (simulating cron ticks)
	res, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, canonical, type) VALUES (?, 'my_cron', 'cron.my_cron', 'cron')`, ppID)
	require.NoError(t, err)
	cronResID, _ := res.LastInsertId()

	// Cron ticks create resource versions — capture actual IDs
	cronVersionIDs := make([]int64, 3)
	for i := 0; i < 3; i++ {
		res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, ?)`,
			cronResID, fmt.Sprintf(`{"date":"tick-%d"}`, i+1))
		require.NoError(t, err)
		cronVersionIDs[i], _ = res.LastInsertId()
	}

	// Create artifact resource + versions (created by gen's PUT step)
	res, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, canonical, type) VALUES (?, 'cron_output', 'artifact.cron_output', 'artifact')`, ppID)
	require.NoError(t, err)
	artifactResID, _ := res.LastInsertId()

	artifactVersionIDs := make([]int64, 3)
	for i := 0; i < 3; i++ {
		res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, ?)`,
			artifactResID, fmt.Sprintf(`{"v":"%d"}`, i+1))
		require.NoError(t, err)
		artifactVersionIDs[i], _ = res.LastInsertId()
	}

	// Create upstream "gen" job
	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'gen')`, ppID)
	require.NoError(t, err)
	genJobID, _ := res.LastInsertId()

	// Gen succeeded 3 times. Each build has:
	// - GET cron (step_name="my_cron", version_id = cron version)
	// - PUT artifact (step_name="cron_output", version_id = artifact version)
	for i := 0; i < 3; i++ {
		res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', ?)`, genJobID, fmt.Sprintf("%d", i+1))
		require.NoError(t, err)
		buildID, _ := res.LastInsertId()
		_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'my_cron', ?)`, buildID, cronVersionIDs[i])
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'cron_output', ?)`, buildID, artifactVersionIDs[i])
		require.NoError(t, err)
	}

	// Create the original deploy-staging (will be "renamed")
	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy-staging')`, ppID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)
	jr := mysql.NewJobRepository(db)

	// --- Test 1: Rename deploy-staging → deploy-staging-v2 (linked via artifact) ---
	newJobID, err := jr.Create(ctx, "main", "cron-rename", job.Job{Name: "deploy-staging-v2"})
	require.NoError(t, err)
	require.NotZero(t, newJobID)

	newJob, err := jr.Find(ctx, "main", "cron-rename", "deploy-staging-v2")
	require.NoError(t, err)
	require.NotNil(t, newJob.BaselineVersionID)
	// Baseline should be the last artifact version (the highest resource_versions.id)
	assert.Equal(t, uint32(artifactVersionIDs[2]), *newJob.BaselineVersionID, "baseline should be MAX(resource_versions.id)")

	// With baseline, all existing artifact versions should be filtered
	_, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "cron-rename",
		[]string{"gen"}, "deploy-staging-v2", "cron_output", 1, newJob.BaselineVersionID)
	require.NoError(t, err)
	assert.False(t, ready, "artifact versions should be filtered by baseline")

	// --- Test 2: Add a monitor job linked to gen via cron (not artifact) ---
	monitorJobID, err := jr.Create(ctx, "main", "cron-rename", job.Job{Name: "monitor"})
	require.NoError(t, err)
	require.NotZero(t, monitorJobID)

	monitorJob, err := jr.Find(ctx, "main", "cron-rename", "monitor")
	require.NoError(t, err)
	require.NotNil(t, monitorJob.BaselineVersionID)

	// With baseline, all existing cron versions should be filtered
	_, ready, err = br.FindReadyDownstreamVersion(ctx, "main", "cron-rename",
		[]string{"gen"}, "monitor", "my_cron", 1, monitorJob.BaselineVersionID)
	require.NoError(t, err)
	assert.False(t, ready, "cron versions should be filtered by baseline")

	// --- Test 3: New cron tick + gen run AFTER baseline → should trigger both ---
	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"date":"tick-4"}')`, cronResID)
	require.NoError(t, err)
	newCronVersionID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"v":"4"}')`, artifactResID)
	require.NoError(t, err)
	newArtifactVersionID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '4')`, genJobID)
	require.NoError(t, err)
	b4ID, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'my_cron', ?)`, b4ID, newCronVersionID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'cron_output', ?)`, b4ID, newArtifactVersionID)
	require.NoError(t, err)

	// deploy-staging-v2 via artifact: new version > baseline → should trigger
	vID, ready, err := br.FindReadyDownstreamVersion(ctx, "main", "cron-rename",
		[]string{"gen"}, "deploy-staging-v2", "cron_output", 1, newJob.BaselineVersionID)
	require.NoError(t, err)
	assert.True(t, ready, "new artifact version after baseline should trigger")
	assert.Equal(t, uint32(newArtifactVersionID), vID)

	// monitor via cron: new version > baseline → should trigger
	vID, ready, err = br.FindReadyDownstreamVersion(ctx, "main", "cron-rename",
		[]string{"gen"}, "monitor", "my_cron", 1, monitorJob.BaselineVersionID)
	require.NoError(t, err)
	assert.True(t, ready, "new cron version after baseline should trigger")
	assert.Equal(t, uint32(newCronVersionID), vID)
}

func TestAggregateStatusByVersionIDs(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'agg-pipe', 'agg-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	// Build 1: succeeded, consumed version 10
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, jobID)
	require.NoError(t, err)
	b1, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, b1)
	require.NoError(t, err)

	// Build 2: failed, consumed version 10 (should make agg status "failed")
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'failed', '2')`, jobID)
	require.NoError(t, err)
	b2, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 10)`, b2)
	require.NoError(t, err)

	// Build 3: succeeded, consumed version 20
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '3')`, jobID)
	require.NoError(t, err)
	b3, _ := res.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO build_get_versions (build_id, step_name, version_id) VALUES (?, 'repo', 20)`, b3)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	result, err := br.AggregateStatusByVersionIDs(ctx, []uint32{10, 20, 30})
	require.NoError(t, err)

	// Version 10: has succeeded + failed → "failed" wins
	assert.Equal(t, "failed", result[10])
	// Version 20: only succeeded → "succeeded"
	assert.Equal(t, "succeeded", result[20])
	// Version 30: no builds → not in map
	_, exists := result[30]
	assert.False(t, exists)
}

func TestAggregateStatusByVersionIDs_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	result, err := br.AggregateStatusByVersionIDs(ctx, []uint32{})
	require.NoError(t, err)
	assert.Nil(t, result)

	result, err = br.AggregateStatusByVersionIDs(ctx, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestCountRunningInSerialGroups(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'sg-pipe', 'sg-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy-staging')`, ppID)
	require.NoError(t, err)
	stagingID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy-prod')`, ppID)
	require.NoError(t, err)
	prodID, _ := res.LastInsertId()

	// Both jobs share serial group "deploy"
	_, err = db.ExecContext(ctx, `INSERT INTO job_serial_groups (job_id, serial_group) VALUES (?, 'deploy')`, stagingID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO job_serial_groups (job_id, serial_group) VALUES (?, 'deploy')`, prodID)
	require.NoError(t, err)

	// deploy-staging has a running build
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'started', '1')`, stagingID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	// From deploy-prod's perspective, there's 1 running build in the serial group
	count, err := br.CountRunningInSerialGroups(ctx, "main", "sg-pipe", []string{"deploy"}, "deploy-prod")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// From deploy-staging's perspective, its own build is excluded
	count, err = br.CountRunningInSerialGroups(ctx, "main", "sg-pipe", []string{"deploy"}, "deploy-staging")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountStarted(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Clean up any started builds from other tests (shared in-memory DB)
	_, _ = db.ExecContext(ctx, `UPDATE builds SET status = 'failed' WHERE status = 'started'`)

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'cs-pipe', 'cs-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	// Verify no started builds initially
	count, err := br.CountStarted(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Insert builds in various statuses
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'started', '1')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'started', '2')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'pending', '3')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '4')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'failed', '5')`, jobID)
	require.NoError(t, err)

	count, err = br.CountStarted(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestFailStartedBuilds(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Clean up any started builds from other tests (shared in-memory DB)
	_, _ = db.ExecContext(ctx, `UPDATE builds SET status = 'failed' WHERE status = 'started'`)

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fsb-pipe', 'fsb-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	// Insert builds in various statuses
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'started', '1')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'started', '2')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'pending', '3')`, jobID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '4')`, jobID)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	n, err := br.FailStartedBuilds(ctx, "server shutdown")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Verify started builds are now failed
	b1, err := br.Find(ctx, "main", "fsb-pipe", "build", "1")
	require.NoError(t, err)
	assert.Equal(t, "failed", b1.Status.String())
	assert.Equal(t, "server shutdown", b1.Error)

	b2, err := br.Find(ctx, "main", "fsb-pipe", "build", "2")
	require.NoError(t, err)
	assert.Equal(t, "failed", b2.Status.String())
	assert.Equal(t, "server shutdown", b2.Error)

	// Verify pending build is untouched
	b3, err := br.Find(ctx, "main", "fsb-pipe", "build", "3")
	require.NoError(t, err)
	assert.Equal(t, "pending", b3.Status.String())

	// Verify succeeded build is untouched
	b4, err := br.Find(ctx, "main", "fsb-pipe", "build", "4")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", b4.Status.String())

	// No started builds remain
	count, err := br.CountStarted(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountRunningInSerialGroups_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	count, err := br.CountRunningInSerialGroups(ctx, "main", "pipe", []string{}, "job")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFilterByPipeline(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fbp-pipe', 'fbp-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	buildJobID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppID)
	require.NoError(t, err)
	testJobID, _ := res.LastInsertId()

	// Insert builds for 'build' job: succeeded, pending
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, buildJobID)
	require.NoError(t, err)
	buildSucceededID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'pending', '2')`, buildJobID)
	require.NoError(t, err)
	buildPendingID, _ := res.LastInsertId()

	// Insert builds for 'test' job: failed, pending
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'failed', '1')`, testJobID)
	require.NoError(t, err)
	testFailedID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'pending', '2')`, testJobID)
	require.NoError(t, err)
	testPendingID, _ := res.LastInsertId()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	t.Run("no status filter returns all builds grouped by job", func(t *testing.T) {
		result, err := br.FilterByPipeline(ctx, "main", "fbp-pipe", nil)
		require.NoError(t, err)

		assert.Contains(t, result, "build")
		assert.Contains(t, result, "test")

		buildBuilds := result["build"]
		assert.Len(t, buildBuilds, 2)
		// Ordered by id DESC: pending (higher id) first
		assert.Equal(t, uint32(buildPendingID), buildBuilds[0].ID)
		assert.Equal(t, uint32(buildSucceededID), buildBuilds[1].ID)

		testBuilds := result["test"]
		assert.Len(t, testBuilds, 2)
		// Ordered by id DESC: pending (higher id) first
		assert.Equal(t, uint32(testPendingID), testBuilds[0].ID)
		assert.Equal(t, uint32(testFailedID), testBuilds[1].ID)
	})

	t.Run("status filter returns only matching builds", func(t *testing.T) {
		result, err := br.FilterByPipeline(ctx, "main", "fbp-pipe", []build.Status{build.Pending})
		require.NoError(t, err)

		assert.Contains(t, result, "build")
		assert.Contains(t, result, "test")

		buildBuilds := result["build"]
		assert.Len(t, buildBuilds, 1)
		assert.Equal(t, uint32(buildPendingID), buildBuilds[0].ID)
		assert.Equal(t, "pending", buildBuilds[0].Status.String())

		testBuilds := result["test"]
		assert.Len(t, testBuilds, 1)
		assert.Equal(t, uint32(testPendingID), testBuilds[0].ID)
		assert.Equal(t, "pending", testBuilds[0].Status.String())
	})

	t.Run("status filter with no matches returns empty map", func(t *testing.T) {
		result, err := br.FilterByPipeline(ctx, "main", "fbp-pipe", []build.Status{build.Started})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("different pipeline not included", func(t *testing.T) {
		result, err := br.FilterByPipeline(ctx, "main", "other-pipe", nil)
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestFilterSummary(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fs-pipe', 'fs-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	steps := `[{"type":"task","name":"run","status":"succeeded","logs":"lots of log data"}]`
	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number, steps) VALUES (?, 'succeeded', '1', ?)`, jobID, steps)
	require.NoError(t, err)
	build1ID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number, steps) VALUES (?, 'failed', '2', ?)`, jobID, steps)
	require.NoError(t, err)

	br := mysql.NewBuildRepository(db, mysql.Mem)

	t.Run("steps are omitted", func(t *testing.T) {
		builds, err := br.FilterSummary(ctx, "main", "fs-pipe", "build", nil, nil, 0, nil)
		require.NoError(t, err)
		require.Len(t, builds, 2)
		assert.Empty(t, builds[0].Steps, "steps should be nil in summary mode")
		assert.Empty(t, builds[1].Steps, "steps should be nil in summary mode")
	})

	t.Run("other fields are populated", func(t *testing.T) {
		builds, err := br.FilterSummary(ctx, "main", "fs-pipe", "build", nil, nil, 0, nil)
		require.NoError(t, err)
		require.Len(t, builds, 2)
		// newest first
		assert.Equal(t, "2", builds[0].BuildNumber)
		assert.Equal(t, build.Failed, builds[0].Status)
		assert.Equal(t, uint32(build1ID), builds[1].ID)
	})

	t.Run("status filter still works", func(t *testing.T) {
		builds, err := br.FilterSummary(ctx, "main", "fs-pipe", "build", nil, nil, 0, []build.Status{build.Succeeded})
		require.NoError(t, err)
		require.Len(t, builds, 1)
		assert.Equal(t, uint32(build1ID), builds[0].ID)
		assert.Empty(t, builds[0].Steps)
	})
}

func TestLatestBuildStatusByPipeline(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'lbs-pipe', 'lbs-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, ppID)
	require.NoError(t, err)
	buildJobID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number, steps) VALUES (?, 'succeeded', '1', '{"big":"data"}')`, buildJobID)
	require.NoError(t, err)
	buildSucceededID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number, steps) VALUES (?, 'pending', '2', '{"big":"data"}')`, buildJobID)
	require.NoError(t, err)
	buildPendingID, _ := res.LastInsertId()

	br := mysql.NewBuildRepository(db, mysql.Mem)

	t.Run("returns builds with only ID, BuildNumber, Status populated", func(t *testing.T) {
		result, err := br.LatestBuildStatusByPipeline(ctx, "main", "lbs-pipe")
		require.NoError(t, err)

		assert.Contains(t, result, "build")
		builds := result["build"]
		assert.Len(t, builds, 2)
		// Ordered by id DESC
		assert.Equal(t, uint32(buildPendingID), builds[0].ID)
		assert.Equal(t, "2", builds[0].BuildNumber)
		assert.Equal(t, build.Pending, builds[0].Status)
		// Steps must not be populated
		assert.Empty(t, builds[0].Steps)

		assert.Equal(t, uint32(buildSucceededID), builds[1].ID)
		assert.Equal(t, "1", builds[1].BuildNumber)
		assert.Equal(t, build.Succeeded, builds[1].Status)
		assert.Empty(t, builds[1].Steps)
	})

	t.Run("waiting_for_approval status is returned correctly", func(t *testing.T) {
		res2, err := db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'waiting_for_approval', '3')`, buildJobID)
		require.NoError(t, err)
		wfaID, _ := res2.LastInsertId()

		result, err := br.LatestBuildStatusByPipeline(ctx, "main", "lbs-pipe")
		require.NoError(t, err)
		builds := result["build"]
		// Newest first: wfa > pending > succeeded
		assert.Equal(t, uint32(wfaID), builds[0].ID)
		assert.Equal(t, build.WaitingForApproval, builds[0].Status)
		assert.Empty(t, builds[0].Steps)
	})

	t.Run("empty result for non-existent pipeline", func(t *testing.T) {
		result, err := br.LatestBuildStatusByPipeline(ctx, "main", "no-such-pipe")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("different pipeline not included", func(t *testing.T) {
		result, err := br.LatestBuildStatusByPipeline(ctx, "main", "other-pipe")
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("different team not included", func(t *testing.T) {
		res2, err := db.ExecContext(ctx, `INSERT INTO teams (name, canonical) VALUES ('lbs-other-team', 'lbs-other')`)
		require.NoError(t, err)
		otherTeamID, _ := res2.LastInsertId()

		res2, err = db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (?, 'lbs-pipe', 'lbs-pipe')`, otherTeamID)
		require.NoError(t, err)
		otherPipeID, _ := res2.LastInsertId()

		res2, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'build')`, otherPipeID)
		require.NoError(t, err)
		otherJobID, _ := res2.LastInsertId()

		_, err = db.ExecContext(ctx, `INSERT INTO builds (job_id, status, build_number) VALUES (?, 'succeeded', '1')`, otherJobID)
		require.NoError(t, err)

		// Query for main team only — should not include the other-team builds
		result, err := br.LatestBuildStatusByPipeline(ctx, "main", "lbs-pipe")
		require.NoError(t, err)
		// Main team has 3 builds (succeeded, pending, wfa); the other team's build must not appear
		assert.Len(t, result["build"], 3)
	})
}
