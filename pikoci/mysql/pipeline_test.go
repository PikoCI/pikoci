package mysql_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/pipeline"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})
	require.NoError(t, err)
	err = migrate.Migrate(db, mysql.Mem)
	require.NoError(t, err)
	return db
}

func TestDeletePipeline_CascadesJobs(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Use the seeded "main" team (id=1, created by migration)
	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'test-pipe', 'test-pipe')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	// Create jobs
	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppID)
	require.NoError(t, err)

	// Verify jobs exist
	var jobCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE pipeline_id = ?`, ppID).Scan(&jobCount)
	require.NoError(t, err)
	assert.Equal(t, 2, jobCount)

	// Delete the pipeline using the repository
	pr := mysql.NewPipelineRepository(db)
	err = pr.Delete(ctx, "main", "test-pipe")
	require.NoError(t, err)

	// Verify cascade: jobs should be deleted
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE pipeline_id = ?`, ppID).Scan(&jobCount)
	require.NoError(t, err)
	assert.Equal(t, 0, jobCount, "jobs should be cascade-deleted when pipeline is deleted")
}

func TestDeletePipeline_CascadesResources(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'cascade-res', 'cascade-res')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, type, canonical) VALUES (?, 'repo', 'git', 'git.repo')`, ppID)
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	err = pr.Delete(ctx, "main", "cascade-res")
	require.NoError(t, err)

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resources WHERE pipeline_id = ?`, ppID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "resources should be cascade-deleted when pipeline is deleted")
}

func TestDeletePipeline_CascadesResourceTypes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'cascade-rt', 'cascade-rt')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO resource_types (pipeline_id, name) VALUES (?, 'git')`, ppID)
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	err = pr.Delete(ctx, "main", "cascade-rt")
	require.NoError(t, err)

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_types WHERE pipeline_id = ?`, ppID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "resource_types should be cascade-deleted when pipeline is deleted")
}

func TestFilterAll_ReturnsPipelinesWithTeam(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create two pipelines under the seeded "main" team
	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical, raw) VALUES (1, 'fa-pipe-a', 'fa-pipe-a', '')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical, raw) VALUES (1, 'fa-pipe-b', 'fa-pipe-b', '')`)
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	pps, err := pr.FilterAll(ctx)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(pps), 2)

	names := make(map[string]string) // pipeline name -> team canonical
	for _, p := range pps {
		names[p.Name] = p.Team.Canonical
	}
	assert.Equal(t, "main", names["fa-pipe-a"])
	assert.Equal(t, "main", names["fa-pipe-b"])
}

func TestFilterAll_IncludesJobs(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical, raw) VALUES (1, 'fa-pipe-jobs', 'fa-pipe-jobs', '')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'lint')`, ppID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'test')`, ppID)
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	pps, err := pr.FilterAll(ctx)
	require.NoError(t, err)

	var found bool
	for _, p := range pps {
		if p.Name == "fa-pipe-jobs" {
			found = true
			assert.Equal(t, 2, len(p.Jobs))
			break
		}
	}
	assert.True(t, found, "fa-pipe-jobs should be in results")
}

func TestDeletePipeline_CascadesRunners(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'cascade-run', 'cascade-run')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO runners (pipeline_id, name) VALUES (?, 'docker')`, ppID)
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	err = pr.Delete(ctx, "main", "cascade-run")
	require.NoError(t, err)

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runners WHERE pipeline_id = ?`, ppID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "runners should be cascade-deleted when pipeline is deleted")
}

func TestJobCreate_PersistsSerialGroups(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'sg-create', 'sg-create')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	j := job.Job{Name: "deploy-staging", SerialGroups: []string{"deploy", "infra"}}
	id, err := jr.Create(ctx, "main", "sg-create", j)
	require.NoError(t, err)
	assert.NotZero(t, id)

	found, err := jr.Find(ctx, "main", "sg-create", "deploy-staging")
	require.NoError(t, err)
	assert.Equal(t, []string{"deploy", "infra"}, found.SerialGroups)
}

func TestJobUpdate_ReplacesSerialGroups(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'sg-update', 'sg-update')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	j := job.Job{Name: "deploy", SerialGroups: []string{"deploy"}}
	_, err = jr.Create(ctx, "main", "sg-update", j)
	require.NoError(t, err)

	j.SerialGroups = []string{"infra"}
	err = jr.Update(ctx, "main", "sg-update", "deploy", j)
	require.NoError(t, err)

	found, err := jr.Find(ctx, "main", "sg-update", "deploy")
	require.NoError(t, err)
	assert.Equal(t, []string{"infra"}, found.SerialGroups)
}

func TestFindJobsBySerialGroups(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'sg-find', 'sg-find')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)

	_, err = jr.Create(ctx, "main", "sg-find", job.Job{Name: "staging", SerialGroups: []string{"deploy"}})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "sg-find", job.Job{Name: "prod", SerialGroups: []string{"deploy"}})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "sg-find", job.Job{Name: "unrelated"})
	require.NoError(t, err)

	jobs, err := jr.FindJobsBySerialGroups(ctx, "main", "sg-find", []string{"deploy"})
	require.NoError(t, err)
	assert.Len(t, jobs, 2)

	names := []string{jobs[0].Name, jobs[1].Name}
	assert.Contains(t, names, "staging")
	assert.Contains(t, names, "prod")
}

func TestFindJobsBySerialGroups_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	jr := mysql.NewJobRepository(db)
	jobs, err := jr.FindJobsBySerialGroups(ctx, "main", "pipe", []string{})
	require.NoError(t, err)
	assert.Nil(t, jobs)
}

func TestJobFilter_IncludesSerialGroups(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'sg-filter', 'sg-filter')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "sg-filter", job.Job{Name: "staging", SerialGroups: []string{"deploy"}})
	require.NoError(t, err)

	jobs, err := jr.Filter(ctx, "main", "sg-filter")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, []string{"deploy"}, jobs[0].SerialGroups)
}

func TestJobCreate_ForEachFields(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fe-create', 'fe-create')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "fe-create", job.Job{
		Name:         "test--1-21",
		ForEachGroup: "test",
		ForEachKey:   "1.21",
	})
	require.NoError(t, err)

	j, err := jr.Find(ctx, "main", "fe-create", "test--1-21")
	require.NoError(t, err)
	assert.Equal(t, "test--1-21", j.Name)
	assert.Equal(t, "test", j.ForEachGroup)
	assert.Equal(t, "1.21", j.ForEachKey)
}

func TestFilterByForEachGroup(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fe-filter', 'fe-filter')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "fe-filter", job.Job{Name: "test--a", ForEachGroup: "test", ForEachKey: "a"})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "fe-filter", job.Job{Name: "test--b", ForEachGroup: "test", ForEachKey: "b"})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "fe-filter", job.Job{Name: "unrelated"})
	require.NoError(t, err)

	jobs, err := jr.FilterByForEachGroup(ctx, "main", "fe-filter", "test")
	require.NoError(t, err)
	assert.Len(t, jobs, 2)
	names := []string{jobs[0].Name, jobs[1].Name}
	assert.Contains(t, names, "test--a")
	assert.Contains(t, names, "test--b")
}

func TestFilterByForEachGroup_Empty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	jr := mysql.NewJobRepository(db)
	jobs, err := jr.FilterByForEachGroup(ctx, "main", "nonexistent", "test")
	require.NoError(t, err)
	assert.Nil(t, jobs)
}

func TestPipelineFind_IncludesForEachFields(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fe-find', 'fe-find')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "fe-find", job.Job{Name: "test--a", ForEachGroup: "test", ForEachKey: "a"})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "fe-find", job.Job{Name: "test--b", ForEachGroup: "test", ForEachKey: "b"})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "fe-find", job.Job{Name: "regular"})
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	pp, err := pr.Find(ctx, "main", "fe-find")
	require.NoError(t, err)
	require.Len(t, pp.Jobs, 3)

	jobMap := make(map[string]job.Job)
	for _, j := range pp.Jobs {
		jobMap[j.Name] = j
	}

	assert.Equal(t, "test", jobMap["test--a"].ForEachGroup)
	assert.Equal(t, "a", jobMap["test--a"].ForEachKey)
	assert.Equal(t, "test", jobMap["test--b"].ForEachGroup)
	assert.Equal(t, "b", jobMap["test--b"].ForEachKey)
	assert.Equal(t, "", jobMap["regular"].ForEachGroup)
	assert.Equal(t, "", jobMap["regular"].ForEachKey)
}

func TestPipelineFilterAll_IncludesForEachFields(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'fe-all', 'fe-all')`)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "fe-all", job.Job{Name: "test--a", ForEachGroup: "test", ForEachKey: "a"})
	require.NoError(t, err)
	_, err = jr.Create(ctx, "main", "fe-all", job.Job{Name: "regular"})
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	pps, err := pr.FilterAll(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, pps)

	// Find our pipeline
	var found *pipeline.WithTeam
	for _, pwt := range pps {
		if pwt.Canonical == "fe-all" {
			found = pwt
			break
		}
	}
	require.NotNil(t, found, "pipeline fe-all not found in FilterAll results")
	require.Len(t, found.Jobs, 2)

	jobMap := make(map[string]job.Job)
	for _, j := range found.Jobs {
		jobMap[j.Name] = j
	}

	assert.Equal(t, "test", jobMap["test--a"].ForEachGroup)
	assert.Equal(t, "a", jobMap["test--a"].ForEachKey)
	assert.Equal(t, "", jobMap["regular"].ForEachGroup)
}

func TestFilterAll_IncludesBaselineVersionID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bl-all', 'bl-all')`)
	require.NoError(t, err)

	// Create a resource version so MAX(resource_versions.id) is non-null
	res, err := db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, canonical, type) VALUES (
		(SELECT id FROM pipelines WHERE canonical = 'bl-all'), 'timer', 'cron.timer', 'cron')`)
	require.NoError(t, err)
	resID, _ := res.LastInsertId()
	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"v":"1"}')`, resID)
	require.NoError(t, err)
	expectedBaseline, _ := res.LastInsertId()

	// Create job via repository — baseline_version_id should be set
	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "bl-all", job.Job{Name: "deploy"})
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	pps, err := pr.FilterAll(ctx)
	require.NoError(t, err)

	var found *pipeline.WithTeam
	for _, pwt := range pps {
		if pwt.Canonical == "bl-all" {
			found = pwt
			break
		}
	}
	require.NotNil(t, found)
	require.Len(t, found.Jobs, 1)
	require.NotNil(t, found.Jobs[0].BaselineVersionID,
		"FilterAll must load baseline_version_id for the scheduler")
	assert.Equal(t, uint32(expectedBaseline), *found.Jobs[0].BaselineVersionID)
}

func TestFind_IncludesBaselineVersionID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'bl-find', 'bl-find')`)
	require.NoError(t, err)

	res, err := db.ExecContext(ctx, `INSERT INTO resources (pipeline_id, name, canonical, type) VALUES (
		(SELECT id FROM pipelines WHERE canonical = 'bl-find'), 'timer', 'cron.timer', 'cron')`)
	require.NoError(t, err)
	resID, _ := res.LastInsertId()
	res, err = db.ExecContext(ctx, `INSERT INTO resource_versions (resource_id, version) VALUES (?, '{"v":"1"}')`, resID)
	require.NoError(t, err)
	expectedBaseline, _ := res.LastInsertId()

	jr := mysql.NewJobRepository(db)
	_, err = jr.Create(ctx, "main", "bl-find", job.Job{Name: "deploy"})
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	pp, err := pr.Find(ctx, "main", "bl-find")
	require.NoError(t, err)
	require.Len(t, pp.Jobs, 1)
	require.NotNil(t, pp.Jobs[0].BaselineVersionID,
		"Find must load baseline_version_id for the scheduler")
	assert.Equal(t, uint32(expectedBaseline), *pp.Jobs[0].BaselineVersionID)
}

func TestDeletePipeline_CascadesSerialGroups(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'sg-cascade', 'sg-cascade')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	res, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'deploy')`, ppID)
	require.NoError(t, err)
	jobID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO job_serial_groups (job_id, serial_group) VALUES (?, 'deploy')`, jobID)
	require.NoError(t, err)

	pr := mysql.NewPipelineRepository(db)
	err = pr.Delete(ctx, "main", "sg-cascade")
	require.NoError(t, err)

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_serial_groups WHERE job_id = ?`, jobID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "serial groups should be cascade-deleted when pipeline is deleted")
}

func TestJobUpdate_PreservesPausedState(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Create a pipeline with a job
	res, err := db.ExecContext(ctx, `INSERT INTO pipelines (team_id, name, canonical) VALUES (1, 'pause-test', 'pause-test')`)
	require.NoError(t, err)
	ppID, _ := res.LastInsertId()

	_, err = db.ExecContext(ctx, `INSERT INTO jobs (pipeline_id, name) VALUES (?, 'my-job')`, ppID)
	require.NoError(t, err)

	jr := mysql.NewJobRepository(db)

	// Pause the job
	err = jr.SetPaused(ctx, "main", "pause-test", "my-job", true)
	require.NoError(t, err)

	// Verify it's paused
	j, err := jr.Find(ctx, "main", "pause-test", "my-job")
	require.NoError(t, err)
	assert.True(t, j.Paused, "job should be paused after SetPaused(true)")

	// Update the job with Paused=false (simulating pipeline update from HCL)
	err = jr.Update(ctx, "main", "pause-test", "my-job", job.Job{
		Name: "my-job",
	})
	require.NoError(t, err)

	// Verify the job is still paused
	j, err = jr.Find(ctx, "main", "pause-test", "my-job")
	require.NoError(t, err)
	assert.True(t, j.Paused, "job should remain paused after Update()")
}
