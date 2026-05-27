//go:build integration

package backends_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/mysql/migrate"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/unitwork"
	"github.com/xescugc/pikoci/pikoci/user"
	"github.com/xescugc/pikoci/worker"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/mempubsub"
)

func TestExportE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", "test-export")

	dbFile := t.TempDir() + "/test.db"
	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.SQLite,
		DBName:          dbFile,
		DBFile:          dbFile,
	})
	require.NoError(t, err)

	err = migrate.Migrate(db, mysql.SQLite)
	require.NoError(t, err)

	jobTopic, err := pubsub.OpenTopic(ctx, fmt.Sprintf("%s://export-test-jobs", mempubsub.Scheme))
	require.NoError(t, err)
	defer jobTopic.Shutdown(ctx)

	checkTopic, err := pubsub.OpenTopic(ctx, fmt.Sprintf("%s://export-test-checks", mempubsub.Scheme))
	require.NoError(t, err)
	defer checkTopic.Shutdown(ctx)

	ur := mysql.NewUserRepository(db)
	tr := mysql.NewTeamRepository(db)
	ppr := mysql.NewPipelineRepository(db)
	jr := mysql.NewJobRepository(db)
	rr := mysql.NewResourceRepository(db, mysql.SQLite)
	rt := mysql.NewResourceTypeRepository(db)
	br := mysql.NewBuildRepository(db, mysql.SQLite)
	rur := mysql.NewRunnerRepository(db)
	str := mysql.NewSecretTypeRepository(db)
	tgr := mysql.NewTriggerRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.SQLite)

	jwtSecret := []byte("test-secret")
	svc := pikoci.New(ctx, jobTopic, checkTopic, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, suow, jwtSecret, logger)
	svc.StartScheduler(ctx)

	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin",
		Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	jobSub, err := pubsub.OpenSubscription(ctx, fmt.Sprintf("%s://export-test-jobs", mempubsub.Scheme))
	require.NoError(t, err)
	defer jobSub.Shutdown(ctx)

	checkSub, err := pubsub.OpenSubscription(ctx, fmt.Sprintf("%s://export-test-checks", mempubsub.Scheme))
	require.NoError(t, err)
	defer checkSub.Shutdown(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := worker.New(svc, jobTopic, jobSub, checkSub, logger.With("worker", 1))
		w.Run(ctx)
	}()

	hclConfig := []byte(`
resource "cron" "trigger" {
  check_interval = "@every 1h"
}

job "export-job" {
  get "cron" "trigger" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo export-job done"]
    }
  }
}
`)

	pp, err := svc.CreatePipeline(ctx, "main", "export-test", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	// Seed a version so the next check creates a second version
	_, err = svc.CreateResourceVersion(ctx, "main", "export-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)

	// Trigger the resource — creates a build
	err = svc.TriggerPipelineResource(ctx, "main", "export-test", "cron.trigger")
	require.NoError(t, err)

	// Wait for build to complete
	require.Eventually(t, func() bool {
		builds, _, err := svc.ListJobBuilds(ctx, "main", "export-test", "export-job", nil, nil, 0)
		if err != nil || len(builds) == 0 {
			return false
		}
		return builds[0].Status == build.Succeeded || builds[0].Status == build.Failed
	}, 30*time.Second, 200*time.Millisecond, "build should complete")

	// Export the database
	migrateFn := func(db *sql.DB, system string) error {
		return migrate.Migrate(db, system)
	}
	exportPath, err := mysql.Export(ctx, db, mysql.SQLite, migrateFn)
	require.NoError(t, err)
	defer os.Remove(exportPath)

	// Open the exported SQLite file
	exportDB, err := mysql.New("", 0, "", "", mysql.Options{
		System: mysql.SQLite,
		DBFile: exportPath,
	})
	require.NoError(t, err)
	defer exportDB.Close()

	// Verify key tables have data
	tables := []struct {
		name     string
		minCount int
	}{
		{"teams", 1},
		{"users", 1},
		{"pipelines", 1},
		{"jobs", 1},
		{"resources", 1},
		{"resource_versions", 2}, // seed + trigger
		{"builds", 1},
	}

	for _, tc := range tables {
		var count int
		err := exportDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", tc.name)).Scan(&count)
		require.NoError(t, err, "table %s", tc.name)
		assert.GreaterOrEqual(t, count, tc.minCount, "table %s should have at least %d rows, got %d", tc.name, tc.minCount, count)
	}

	// Verify specific data: pipeline name
	var pipelineName string
	err = exportDB.QueryRow("SELECT name FROM pipelines WHERE canonical = 'export-test'").Scan(&pipelineName)
	require.NoError(t, err)
	assert.Equal(t, "export-test", pipelineName)

	// Verify the exported DB can be used by creating repositories from it
	exportUR := mysql.NewUserRepository(exportDB)
	u, err := exportUR.Find(ctx, "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", u.Username)

	cancel()
	wg.Wait()
}
