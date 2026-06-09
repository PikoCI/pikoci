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
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/mempubsub"
)

func TestServicesE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", "test-services")

	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})
	require.NoError(t, err)

	err = migrate.Migrate(db, mysql.Mem)
	require.NoError(t, err)

	jobTopic, err := pubsub.OpenTopic(ctx, fmt.Sprintf("%s://services-test-jobs", mempubsub.Scheme))
	require.NoError(t, err)
	defer jobTopic.Shutdown(ctx)

	checkTopic, err := pubsub.OpenTopic(ctx, fmt.Sprintf("%s://services-test-checks", mempubsub.Scheme))
	require.NoError(t, err)
	defer checkTopic.Shutdown(ctx)

	ur := mysql.NewUserRepository(db)
	tr := mysql.NewTeamRepository(db)
	ppr := mysql.NewPipelineRepository(db)
	jr := mysql.NewJobRepository(db)
	rr := mysql.NewResourceRepository(db, mysql.Mem)
	rt := mysql.NewResourceTypeRepository(db)
	br := mysql.NewBuildRepository(db, mysql.Mem)
	rur := mysql.NewRunnerRepository(db)
	str := mysql.NewSecretTypeRepository(db)
	tgr := mysql.NewTriggerRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.Mem)

	jwtSecret := []byte("test-secret")
	svc := pikoci.New(ctx, jobTopic, checkTopic, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, suow, jwtSecret, logger)
	svc.StartScheduler(ctx)

	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin",
		Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	jobSub, err := pubsub.OpenSubscription(ctx, fmt.Sprintf("%s://services-test-jobs", mempubsub.Scheme))
	require.NoError(t, err)
	defer jobSub.Shutdown(ctx)

	checkSub, err := pubsub.OpenSubscription(ctx, fmt.Sprintf("%s://services-test-checks", mempubsub.Scheme))
	require.NoError(t, err)
	defer checkSub.Shutdown(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := worker.New(svc, jobTopic, jobSub, checkSub, logger.With("component", "worker"), "test-worker", "jobs,checks", "test", 1)
		w.Run(ctx)
	}()

	t.Run("ServiceStartAndStop", func(t *testing.T) {
		tmpDir := t.TempDir()
		markerFile := tmpDir + "/service-marker"

		hclConfig := []byte(fmt.Sprintf(`
service_type "test-svc" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "touch %s && echo started"]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "test -f %s"]
    interval = "500ms"
    timeout  = "5s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "rm -f %s && echo stopped"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "use-service" {
  service "test-svc" {}

  get "cron" "timer" {
    trigger = true
  }
  task "check-marker" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "test -f %s && echo marker_exists"]
    }
  }
}
`, markerFile, markerFile, markerFile, markerFile))

		pp, err := svc.CreatePipeline(ctx, "main", "svc-start-stop-e2e", hclConfig, nil)
		require.NoError(t, err)
		require.NotNil(t, pp)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "svc-start-stop-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		err = svc.TriggerPipelineResource(ctx, "main", "svc-start-stop-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "svc-start-stop-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "svc-start-stop-e2e", "cron.timer")
		require.NoError(t, err)

		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "svc-start-stop-e2e", "use-service", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			return builds[0].Status != build.Started && builds[0].Status != build.Pending
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)

		// Verify service steps exist
		var startStep, readyStep, stopStep *build.Step
		for i, s := range b.Steps {
			switch s.Name {
			case "test-svc:start":
				startStep = &b.Steps[i]
			case "test-svc:ready":
				readyStep = &b.Steps[i]
			case "test-svc:stop":
				stopStep = &b.Steps[i]
			}
		}
		require.NotNil(t, startStep, "start step should exist")
		require.NotNil(t, readyStep, "ready step should exist")
		require.NotNil(t, stopStep, "stop step should exist")
		assert.Contains(t, startStep.Logs, "started")
		assert.Contains(t, stopStep.Logs, "stopped")
	})

	t.Run("ServiceReadyCheckTimeout", func(t *testing.T) {
		hclConfig := []byte(`
service_type "slow-svc" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo started"]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "exit 1"]
    interval = "500ms"
    timeout  = "2s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo stopped"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "use-slow-service" {
  service "slow-svc" {}

  get "cron" "timer" {
    trigger = true
  }
  task "should-not-run" {
    run "exec" {
      path = "echo"
      args = ["should not reach here"]
    }
  }
}
`)

		_, err := svc.CreatePipeline(ctx, "main", "svc-timeout-e2e", hclConfig, nil)
		require.NoError(t, err)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "svc-timeout-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		err = svc.TriggerPipelineResource(ctx, "main", "svc-timeout-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "svc-timeout-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "svc-timeout-e2e", "cron.timer")
		require.NoError(t, err)

		// Wait until the build completes AND the stop step is present
		// (stop runs via defer after failBuild, so there's a brief window)
		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "svc-timeout-e2e", "use-slow-service", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			if builds[0].Status == build.Started {
				return false
			}
			for _, s := range builds[0].Steps {
				if s.Name == "slow-svc:stop" {
					return true
				}
			}
			return false
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Failed, b.Status, "build should fail when ready_check times out")

		// Stop should still be called
		var stopStep *build.Step
		for i, s := range b.Steps {
			if s.Name == "slow-svc:stop" {
				stopStep = &b.Steps[i]
				break
			}
		}
		require.NotNil(t, stopStep, "stop step should exist even after ready_check timeout")
	})

	t.Run("ServiceWithParams", func(t *testing.T) {
		tmpDir := t.TempDir()
		versionFile := tmpDir + "/version"

		hclConfig := []byte(fmt.Sprintf(`
service_type "param-svc" {
  params = ["version"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo $param_version > %s"]
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "rm -f %s"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "use-param-service" {
  service "param-svc" {
    version = "15"
  }

  get "cron" "timer" {
    trigger = true
  }
  task "check-version" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "cat %s"]
    }
  }
}
`, versionFile, versionFile, versionFile))

		_, err := svc.CreatePipeline(ctx, "main", "svc-params-e2e", hclConfig, nil)
		require.NoError(t, err)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "svc-params-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		err = svc.TriggerPipelineResource(ctx, "main", "svc-params-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "svc-params-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "svc-params-e2e", "cron.timer")
		require.NoError(t, err)

		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "svc-params-e2e", "use-param-service", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			return builds[0].Status != build.Started && builds[0].Status != build.Pending
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)

		// Task should have read the version file
		var taskStep *build.Step
		for i, s := range b.Steps {
			if s.Type == "task" && s.Name == "check-version" {
				taskStep = &b.Steps[i]
				break
			}
		}
		require.NotNil(t, taskStep, "task step should exist")
		assert.Contains(t, taskStep.Logs, "15", "logs should contain param value 15")
	})

	t.Run("ServiceStopOnTaskFailure", func(t *testing.T) {
		hclConfig := []byte(`
service_type "fail-svc" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo started"]
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo stopped_after_failure"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "will-fail" {
  service "fail-svc" {}

  get "cron" "timer" {
    trigger = true
  }
  task "failing-task" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "exit 1"]
    }
  }
}
`)

		_, err := svc.CreatePipeline(ctx, "main", "svc-stop-fail-e2e", hclConfig, nil)
		require.NoError(t, err)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "svc-stop-fail-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		err = svc.TriggerPipelineResource(ctx, "main", "svc-stop-fail-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "svc-stop-fail-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "svc-stop-fail-e2e", "cron.timer")
		require.NoError(t, err)

		// Wait until the build completes AND the stop step is present
		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "svc-stop-fail-e2e", "will-fail", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			if builds[0].Status == build.Started {
				return false
			}
			for _, s := range builds[0].Steps {
				if s.Name == "fail-svc:stop" {
					return true
				}
			}
			return false
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Failed, b.Status, "build should fail")

		// Stop should still be called
		var stopStep *build.Step
		for i, s := range b.Steps {
			if s.Name == "fail-svc:stop" {
				stopStep = &b.Steps[i]
				break
			}
		}
		require.NotNil(t, stopStep, "stop step should exist even when task fails")
		assert.Contains(t, stopStep.Logs, "stopped_after_failure")
	})

	cancel()
	wg.Wait()
}
