//go:build integration

package backends_test

import (
	"context"
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

func newTagsTestService(t *testing.T, ctx context.Context, logger *slog.Logger) pikoci.Service {
	t.Helper()
	dbFile := t.TempDir() + "/test.db"
	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.SQLite,
		DBName:          dbFile,
		DBFile:          dbFile,
	})
	require.NoError(t, err)
	require.NoError(t, migrate.Migrate(db, mysql.SQLite))

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

	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, suow, []byte("test"), notifier.New(), logger)
	svc.StartScheduler(ctx)

	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin", Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	return svc
}

// TestTaggedJobOnlyRunsOnMatchingWorker verifies that a job tagged "gpu" is
// NOT picked up by an untagged worker but IS picked up by a worker with the
// "gpu" tag.
func TestTaggedJobOnlyRunsOnMatchingWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "tagged-job")
	svc := newTagsTestService(t, ctx, logger)

	hcl := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1h"
}

job "gpu-job" {
  tags = ["gpu"]
  get "cron" "tick" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo gpu-job done"]
    }
  }
}
`)
	pp, err := svc.CreatePipeline(ctx, "main", "tags-test", hcl, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	// Seed a version so TriggerPipelineJob can pin it
	_, err = svc.CreateResourceVersion(ctx, "main", "tags-test", "cron.tick", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)

	// Start an UNTAGGED worker — should NOT pick up the gpu-tagged job
	var wg sync.WaitGroup
	wg.Add(1)
	untaggedWorker := worker.New(svc, logger.With("worker", "untagged"), "untagged-worker", "test", 1, nil, false)
	go func() { defer wg.Done(); untaggedWorker.Run(ctx) }()

	// Directly trigger the job to create a pending build
	err = svc.TriggerPipelineJob(ctx, "main", "tags-test", "gpu-job")
	require.NoError(t, err)

	// Wait a bit — the build should stay pending because no matching worker
	time.Sleep(3 * time.Second)
	builds, _, err := svc.ListJobBuilds(ctx, "main", "tags-test", "gpu-job", nil, nil, 0)
	require.NoError(t, err)
	require.NotEmpty(t, builds, "build should exist")
	assert.Equal(t, build.Pending, builds[0].Status, "build should stay pending without a gpu worker")

	// Now start a GPU-tagged worker — should pick up the build
	wg.Add(1)
	gpuWorker := worker.New(svc, logger.With("worker", "gpu"), "gpu-worker", "test", 1, []string{"gpu"}, false)
	go func() { defer wg.Done(); gpuWorker.Run(ctx) }()

	require.Eventually(t, func() bool {
		builds, _, err := svc.ListJobBuilds(ctx, "main", "tags-test", "gpu-job", nil, nil, 0)
		if err != nil || len(builds) == 0 {
			return false
		}
		return builds[0].Status == build.Succeeded || builds[0].Status == build.Failed
	}, 20*time.Second, 200*time.Millisecond, "gpu worker should complete the build")

	builds, _, _ = svc.ListJobBuilds(ctx, "main", "tags-test", "gpu-job", nil, nil, 0)
	assert.Equal(t, build.Succeeded, builds[0].Status)

	cancel()
	wg.Wait()
}

// TestExclusiveWorkerSkipsUntaggedJobs verifies that a worker with
// --exclusive-tags only picks up jobs matching its tags and ignores
// untagged jobs.
func TestExclusiveWorkerSkipsUntaggedJobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "exclusive")
	svc := newTagsTestService(t, ctx, logger)

	hcl := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1h"
}

job "untagged-job" {
  get "cron" "tick" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo untagged done"]
    }
  }
}

job "gpu-job" {
  tags = ["gpu"]
  get "cron" "tick" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo gpu done"]
    }
  }
}
`)
	pp, err := svc.CreatePipeline(ctx, "main", "excl-test", hcl, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	_, err = svc.CreateResourceVersion(ctx, "main", "excl-test", "cron.tick", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)

	// Start an exclusive gpu worker — should only handle gpu-tagged jobs
	var wg sync.WaitGroup
	wg.Add(1)
	exclWorker := worker.New(svc, logger.With("worker", "exclusive-gpu"), "exclusive-gpu-worker", "test", 1, []string{"gpu"}, true)
	go func() { defer wg.Done(); exclWorker.Run(ctx) }()

	// Directly trigger both jobs to create pending builds
	err = svc.TriggerPipelineJob(ctx, "main", "excl-test", "gpu-job")
	require.NoError(t, err)
	err = svc.TriggerPipelineJob(ctx, "main", "excl-test", "untagged-job")
	require.NoError(t, err)

	// Wait for the gpu-job to complete
	require.Eventually(t, func() bool {
		builds, _, err := svc.ListJobBuilds(ctx, "main", "excl-test", "gpu-job", nil, nil, 0)
		if err != nil || len(builds) == 0 {
			return false
		}
		return builds[0].Status == build.Succeeded || builds[0].Status == build.Failed
	}, 20*time.Second, 200*time.Millisecond, "exclusive worker should complete gpu-job")

	gpuBuilds, _, _ := svc.ListJobBuilds(ctx, "main", "excl-test", "gpu-job", nil, nil, 0)
	assert.Equal(t, build.Succeeded, gpuBuilds[0].Status)

	// The untagged job should still be pending — exclusive worker skips it
	untaggedBuilds, _, err := svc.ListJobBuilds(ctx, "main", "excl-test", "untagged-job", nil, nil, 0)
	require.NoError(t, err)
	require.NotEmpty(t, untaggedBuilds, "untagged build should exist")
	assert.Equal(t, build.Pending, untaggedBuilds[0].Status, "untagged job should remain pending with exclusive worker")

	cancel()
	wg.Wait()
}
