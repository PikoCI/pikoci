//go:build integration

package backends_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
)

// TestConcurrentBuildCreation verifies that when a resource triggers multiple
// jobs simultaneously, all builds are created successfully without SQLITE_BUSY
// errors. This reproduces the production scenario where concurrent workers
// contend on the SQLite write lock. Without _txlock=immediate on the SQLite
// DSN, this test may intermittently fail on slower I/O (e.g. ARM64 servers)
// with only 1 of 3 builds being created due to SQLITE_BUSY.
func TestConcurrentBuildCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", "test-concurrent")

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
	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, nil, nil, suow, jwtSecret, notifier.New(), logger)
	svc.StartScheduler(ctx)

	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin",
		Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	// Start 3 concurrent workers (same as production CONCURRENCY=3)
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := worker.New(svc, logger.With("worker", i+1), fmt.Sprintf("test-worker-%d", i+1), "test", "", 3, nil, false)
			w.Run(ctx)
		}()
	}

	// Pipeline with 3 jobs all triggered by the same resource
	hclConfig := []byte(`
resource "cron" "trigger" {
  check_interval = "@every 1h"
}

job "job-a" {
  get "cron" "trigger" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo job-a done"]
    }
  }
}

job "job-b" {
  get "cron" "trigger" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo job-b done"]
    }
  }
}

job "job-c" {
  get "cron" "trigger" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo job-c done"]
    }
  }
}
`)

	pp, err := svc.CreatePipeline(ctx, "main", "concurrent-test", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	// Seed a version so the next check creates a second version.
	_, err = svc.CreateResourceVersion(ctx, "main", "concurrent-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)

	// Trigger the resource — this creates a version which triggers all 3 jobs
	err = svc.TriggerPipelineResource(ctx, "main", "concurrent-test", "cron.trigger")
	require.NoError(t, err)

	// Wait for all 3 jobs to have at least one completed build
	jobs := []string{"job-a", "job-b", "job-c"}
	for _, jn := range jobs {
		jn := jn
		require.Eventually(t, func() bool {
			builds, _, err := svc.ListJobBuilds(ctx, "main", "concurrent-test", jn, nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			return builds[0].Status != build.Started && builds[0].Status != build.Pending
		}, 15*time.Second, 200*time.Millisecond, "job %q should have a completed build", jn)
	}

	// Verify all 3 builds succeeded
	for _, jn := range jobs {
		builds, _, err := svc.ListJobBuilds(ctx, "main", "concurrent-test", jn, nil, nil, 0)
		require.NoError(t, err)
		require.NotEmpty(t, builds, "job %q should have builds", jn)
		assert.Equal(t, build.Succeeded, builds[0].Status, "job %q build should succeed, error: %s", jn, builds[0].Error)
	}
}
