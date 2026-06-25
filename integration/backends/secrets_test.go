//go:build integration

package backends_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

func TestSecretsE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", "test")

	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})
	require.NoError(t, err)

	err = migrate.Migrate(db, mysql.Mem)
	require.NoError(t, err)

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
	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, suow, jwtSecret, notifier.New(), logger)
	svc.StartScheduler(ctx)

	// Migration already creates admin user and "main" team.
	// Create a test user if admin doesn't exist yet (ignore duplicate error).
	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin",
		Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	// Start worker
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := worker.New(svc, logger.With("component", "worker"), "test-worker", "test", "", 1, nil, false)
		w.Run(ctx)
	}()

	t.Run("SecretBackedVariable", func(t *testing.T) {
		// This pipeline has an inline secret_type and a secret-backed variable.
		// The variable resolves from the secret type and is used in resource params.
		hclConfig := []byte(`
secret_type "mock-vault" {
  params = ["path"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '{\"username\":\"admin\",\"password\":\"s3cret\"}'"]
  }
}

variable "db_username" {
  type = string
  secret "mock-vault" {
    path = "secret/data/db"
    key  = "username"
  }
}

variable "db_password" {
  type = string
  secret "mock-vault" {
    path = "secret/data/db"
    key  = "password"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "use-secrets" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo db_username=${var.db_username} db_password=${var.db_password}"]
    }
  }
}
`)
		pp, err := svc.CreatePipeline(ctx, "main", "secrets-e2e", hclConfig, nil)
		require.NoError(t, err)
		require.NotNil(t, pp)
		assert.Len(t, pp.SecretTypes, 1)
		assert.Equal(t, "mock-vault", pp.SecretTypes[0].Name)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "secrets-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		// Trigger resource check to create a version and trigger builds
		err = svc.TriggerPipelineResource(ctx, "main", "secrets-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "secrets-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "secrets-e2e", "cron.timer")
		require.NoError(t, err)

		// Wait for the build triggered by the resource to finish
		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "secrets-e2e", "deploy", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			return builds[0].Status != build.Started && builds[0].Status != build.Pending
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)

		var taskStep *build.Step
		for i, s := range b.Steps {
			if s.Type == "task" && s.Name == "use-secrets" {
				taskStep = &b.Steps[i]
			}
		}

		require.NotNil(t, taskStep, "task step 'use-secrets' should exist in build steps")
		assert.True(t, strings.Contains(taskStep.Logs, "db_username=***"), "logs should mask db_username value, got: %s", taskStep.Logs)
		assert.True(t, strings.Contains(taskStep.Logs, "db_password=***"), "logs should mask db_password value, got: %s", taskStep.Logs)
		assert.False(t, strings.Contains(taskStep.Logs, "admin"), "logs should not contain raw secret 'admin', got: %s", taskStep.Logs)
		assert.False(t, strings.Contains(taskStep.Logs, "s3cret"), "logs should not contain raw secret 's3cret', got: %s", taskStep.Logs)
	})

	t.Run("SecretBackedVariableWithFileSource", func(t *testing.T) {
		// Uses the built-in "file" secret_type via source
		tmpDir := t.TempDir()
		secretFile := tmpDir + "/secret.json"
		err := os.WriteFile(secretFile, []byte(`{"api_key":"abc123","api_secret":"xyz789"}`), 0644)
		require.NoError(t, err)

		hclConfig := []byte(fmt.Sprintf(`
secret_type "my-file" {
  source = "pikoci://file"
}

variable "api_key" {
  type = string
  secret "my-file" {
    path = "%s"
    key  = "api_key"
  }
}

variable "api_secret" {
  type = string
  secret "my-file" {
    path = "%s"
    key  = "api_secret"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "use-file-secrets" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo api_key=${var.api_key} api_secret=${var.api_secret}"]
    }
  }
}
`, secretFile, secretFile))

		pp, err := svc.CreatePipeline(ctx, "main", "secrets-file-e2e", hclConfig, nil)
		require.NoError(t, err)
		require.NotNil(t, pp)
		assert.Len(t, pp.SecretTypes, 1)
		assert.Equal(t, "my-file", pp.SecretTypes[0].Name)
		assert.Equal(t, "pikoci://file", pp.SecretTypes[0].Source)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "secrets-file-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		// Trigger resource check to create a version and trigger builds
		err = svc.TriggerPipelineResource(ctx, "main", "secrets-file-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "secrets-file-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "secrets-file-e2e", "cron.timer")
		require.NoError(t, err)

		// Wait for the build triggered by the resource to finish
		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "secrets-file-e2e", "deploy", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			return builds[0].Status != build.Started && builds[0].Status != build.Pending
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)

		var taskStep *build.Step
		for i, s := range b.Steps {
			if s.Type == "task" && s.Name == "use-file-secrets" {
				taskStep = &b.Steps[i]
				break
			}
		}
		require.NotNil(t, taskStep, "task step 'use-file-secrets' should exist in build steps")
		assert.True(t, strings.Contains(taskStep.Logs, "api_key=***"), "logs should mask api_key value, got: %s", taskStep.Logs)
		assert.True(t, strings.Contains(taskStep.Logs, "api_secret=***"), "logs should mask api_secret value, got: %s", taskStep.Logs)
		assert.False(t, strings.Contains(taskStep.Logs, "abc123"), "logs should not contain raw secret 'abc123', got: %s", taskStep.Logs)
		assert.False(t, strings.Contains(taskStep.Logs, "xyz789"), "logs should not contain raw secret 'xyz789', got: %s", taskStep.Logs)
	})

	t.Run("SecretBackedVariableWithFileSourceEnvFormat", func(t *testing.T) {
		// Uses the built-in "file" secret_type with format = "env"
		tmpDir := t.TempDir()
		secretFile := tmpDir + "/secret.env"
		err := os.WriteFile(secretFile, []byte("# Database credentials\nDB_HOST=db.example.com\nDB_PASSWORD=s3cret\nDB_USER=\"admin\"\nDB_CONN=host=db;port=5432\n"), 0644)
		require.NoError(t, err)

		hclConfig := []byte(fmt.Sprintf(`
secret_type "env-file" {
  source = "pikoci://file"
  format = "env"
}

variable "db_host" {
  type = string
  secret "env-file" {
    path = "%s"
    key  = "DB_HOST"
  }
}

variable "db_password" {
  type = string
  secret "env-file" {
    path = "%s"
    key  = "DB_PASSWORD"
  }
}

variable "db_user" {
  type = string
  secret "env-file" {
    path = "%s"
    key  = "DB_USER"
  }
}

variable "db_conn" {
  type = string
  secret "env-file" {
    path = "%s"
    key  = "DB_CONN"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "use-env-secrets" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo 'db_host=${var.db_host}' 'db_password=${var.db_password}' 'db_user=${var.db_user}' 'db_conn=${var.db_conn}'"]
    }
  }
}
`, secretFile, secretFile, secretFile, secretFile))

		pp, err := svc.CreatePipeline(ctx, "main", "secrets-env-file-e2e", hclConfig, nil)
		require.NoError(t, err)
		require.NotNil(t, pp)
		assert.Len(t, pp.SecretTypes, 1)
		assert.Equal(t, "env-file", pp.SecretTypes[0].Name)
		assert.Equal(t, "pikoci://file", pp.SecretTypes[0].Source)

		// Seed a version so the next check creates a second version
		_, err = svc.CreateResourceVersion(ctx, "main", "secrets-env-file-e2e", "cron.timer", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		require.NoError(t, err)

		// Trigger resource check to create a version and trigger builds
		err = svc.TriggerPipelineResource(ctx, "main", "secrets-env-file-e2e", "cron.timer")
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			vers, _, err := svc.ListResourceVersions(ctx, "main", "secrets-env-file-e2e", "cron.timer", nil, nil, 0)
			return err == nil && len(vers) > 1
		}, 10*time.Second, 200*time.Millisecond)

		// Second trigger sees existing versions and triggers builds
		err = svc.TriggerPipelineResource(ctx, "main", "secrets-env-file-e2e", "cron.timer")
		require.NoError(t, err)

		// Wait for the build triggered by the resource to finish
		var builds []*build.Build
		require.Eventually(t, func() bool {
			builds, _, err = svc.ListJobBuilds(ctx, "main", "secrets-env-file-e2e", "deploy", nil, nil, 0)
			if err != nil || len(builds) == 0 {
				return false
			}
			return builds[0].Status != build.Started && builds[0].Status != build.Pending
		}, 15*time.Second, 200*time.Millisecond)

		require.NotEmpty(t, builds)
		b := builds[0]
		assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)

		var taskStep *build.Step
		for i, s := range b.Steps {
			if s.Type == "task" && s.Name == "use-env-secrets" {
				taskStep = &b.Steps[i]
				break
			}
		}
		require.NotNil(t, taskStep, "task step 'use-env-secrets' should exist in build steps")
		assert.True(t, strings.Contains(taskStep.Logs, "db_host=***"), "logs should mask db_host value, got: %s", taskStep.Logs)
		assert.True(t, strings.Contains(taskStep.Logs, "db_password=***"), "logs should mask db_password value, got: %s", taskStep.Logs)
		assert.True(t, strings.Contains(taskStep.Logs, "db_user=***"), "logs should mask db_user value, got: %s", taskStep.Logs)
		assert.True(t, strings.Contains(taskStep.Logs, "db_conn=***"), "logs should mask db_conn value, got: %s", taskStep.Logs)
		assert.False(t, strings.Contains(taskStep.Logs, "db.example.com"), "logs should not contain raw secret 'db.example.com', got: %s", taskStep.Logs)
		assert.False(t, strings.Contains(taskStep.Logs, "s3cret"), "logs should not contain raw secret 's3cret', got: %s", taskStep.Logs)
	})

	cancel()
	wg.Wait()
}
