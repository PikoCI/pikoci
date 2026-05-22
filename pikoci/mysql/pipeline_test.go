package mysql_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/mysql/migrate"
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
