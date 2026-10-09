//go:build integration

package backends_test

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFullService wires the service the way cmd/server.go does, on any
// backend: PostgreSQL gets the placeholder-rewriting querier, and the unit of
// work gets the raw *sql.DB.
func newFullService(t *testing.T, ctx context.Context, system string, logger *slog.Logger) *pikoci.PikoCI {
	t.Helper()
	setup := openDB(t, system)
	migrateDB(t, setup)
	q := setup.querier

	svc := pikoci.New(ctx,
		mysql.NewUserRepository(q), mysql.NewTeamRepository(q), mysql.NewPipelineRepository(q),
		mysql.NewJobRepository(q), mysql.NewResourceRepository(q, system), mysql.NewResourceTypeRepository(q),
		mysql.NewBuildRepository(q, system), mysql.NewRunnerRepository(q), mysql.NewSecretTypeRepository(q),
		mysql.NewTriggerRepository(q), nil, nil, nil, nil,
		unitwork.NewStartUnitOfWork(setup.db, system), []byte("test-secret"), notifier.New(), logger)
	svc.StartScheduler(ctx)
	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin", Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)
	return svc
}

// fullServicePipeline declares one of every entity UpdatePipeline rewrites
// (pipeline, job, resource, resource type, runner, secret type, notification
// type, notification), so applying it twice runs each repository's Update.
func fullServicePipeline(tag string, concurrency int) []byte {
	return []byte(`
runner_type "sh" {
  run {
    path = "/bin/sh"
    args = ["-ec", "echo runner-` + tag + `"]
  }
}

resource_type "tick" {
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo \"[{\\\"ref\\\":\\\"$(date +%s%N)\\\"}]\""]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo pulled $version_ref"]
  }
  push "exec" {}
}

resource "tick" "repo" {
  check_interval = "@every 1h"
}

secret_type "vault" {
  params = ["path"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '{\"token\":\"s3cr3t-` + tag + `\"}'"]
  }
}

variable "token" {
  type = string
  secret "vault" {
    path = "secret/` + tag + `"
    key  = "token"
  }
}

notification_type "log" {
  params = ["channel"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notify $param_channel"]
  }
}

notification "log" "ci" {
  message = "build ` + tag + `"
  params {
    channel = "#` + tag + `"
  }
}

job "build" {
  concurrency = ` + strconv.Itoa(concurrency) + `
  get "tick" "repo" {
    trigger = true
  }
  task "use-secret" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "test -n \"${var.token}\""]
    }
  }
  notify "log" "ci" {}
}
`)
}

// TestFullServiceE2E runs the service end to end on every configured backend.
// It exists because the backends were only ever tested repository by
// repository, which never ran an update or scanned a time column on MySQL or
// PostgreSQL, so the full service was broken on both without anyone noticing.
func TestFullServiceE2E(t *testing.T) {
	for _, system := range dbSystems() {
		t.Run(system, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).With("test", "full-service", "db", system)
			svc := newFullService(t, ctx, system, logger)

			w := worker.New(svc, logger.With("component", "worker"), "test-worker", "test", "", 1, nil, false)
			go w.Run(ctx)

			const pc = "full-service"
			waitBuilds := func(t *testing.T, n int) []*build.Build {
				t.Helper()
				var builds []*build.Build
				require.Eventually(t, func() bool {
					var err error
					builds, _, err = svc.ListJobBuilds(ctx, "main", pc, "build", nil, nil, 0, nil)
					if err != nil || len(builds) < n {
						return false
					}
					for _, b := range builds {
						if b.Status == build.Pending || b.Status == build.Started {
							return false
						}
					}
					return true
				}, 30*time.Second, 200*time.Millisecond, "expected %d finished builds", n)
				return builds
			}

			// CreatePipeline runs in a unit of work (transaction).
			_, err := svc.CreatePipeline(ctx, "main", pc, fullServicePipeline("v1", 1), nil)
			require.NoError(t, err)

			t.Run("CheckAndBuild", func(t *testing.T) {
				// RequestCheck, FilterDueResources (time scan), ClaimResourceCheck,
				// version creation, then the build's own updates.
				require.NoError(t, svc.TriggerPipelineResource(ctx, "main", pc, "tick.repo"))
				builds := waitBuilds(t, 1)
				assert.Equal(t, build.Succeeded, builds[0].Status, "error: %s", builds[0].Error)
				assert.NotEmpty(t, builds[0].Steps, "build updates must persist steps")
			})

			t.Run("UpdatePipeline", func(t *testing.T) {
				// Every entity already exists, so each repository's Update runs.
				_, err := svc.UpdatePipeline(ctx, "main", pc, fullServicePipeline("v2", 2), nil)
				require.NoError(t, err)

				j, err := svc.GetPipelineJob(ctx, "main", pc, "build")
				require.NoError(t, err)
				assert.Equal(t, 2, j.Concurrency)

				pp, err := svc.GetPipeline(ctx, "main", pc)
				require.NoError(t, err)
				st, ok := pp.SecretType("vault")
				require.True(t, ok)
				assert.Contains(t, st.Get.Args[1], "s3cr3t-v2")
				n, ok := pp.Notification("log.ci")
				require.True(t, ok)
				assert.Equal(t, "build v2", n.Message)

				// The updated pipeline still runs.
				require.NoError(t, svc.TriggerPipelineResource(ctx, "main", pc, "tick.repo"))
				builds := waitBuilds(t, 2)
				assert.Equal(t, build.Succeeded, builds[0].Status, "error: %s", builds[0].Error)
			})

			t.Run("PinAndUnpin", func(t *testing.T) {
				vers, _, err := svc.ListResourceVersions(ctx, "main", pc, "tick.repo", nil, nil, 0)
				require.NoError(t, err)
				require.NotEmpty(t, vers)

				require.NoError(t, svc.PinResourceVersion(ctx, "main", pc, "tick.repo", vers[len(vers)-1].ID))
				r, err := svc.GetPipelineResource(ctx, "main", pc, "tick.repo")
				require.NoError(t, err)
				require.NotNil(t, r.PinnedVersionID)
				assert.Equal(t, vers[len(vers)-1].ID, *r.PinnedVersionID)

				require.NoError(t, svc.UnpinResourceVersion(ctx, "main", pc, "tick.repo"))
				r, err = svc.GetPipelineResource(ctx, "main", pc, "tick.repo")
				require.NoError(t, err)
				assert.Nil(t, r.PinnedVersionID)
			})

			t.Run("CheckLogs", func(t *testing.T) {
				require.NoError(t, svc.UpdateResourceCheckLogs(ctx, "main", pc, "tick.repo", "check failed: boom"))
				r, err := svc.GetPipelineResource(ctx, "main", pc, "tick.repo")
				require.NoError(t, err)
				assert.Equal(t, "check failed: boom", r.Logs)
			})

			t.Run("PauseAndUnpause", func(t *testing.T) {
				paused := func() bool {
					j, err := svc.GetPipelineJob(ctx, "main", pc, "build")
					require.NoError(t, err)
					return j.Paused
				}
				require.NoError(t, svc.PauseJob(ctx, "main", pc, "build"))
				assert.True(t, paused())
				require.NoError(t, svc.UnpauseJob(ctx, "main", pc, "build"))
				assert.False(t, paused())

				require.NoError(t, svc.PausePipeline(ctx, "main", pc))
				assert.True(t, paused())
				require.NoError(t, svc.UnpausePipeline(ctx, "main", pc))
				assert.False(t, paused())
			})

			t.Run("SetPublic", func(t *testing.T) {
				require.NoError(t, svc.SetPipelinePublic(ctx, "main", pc, true))
				pp, err := svc.GetPipeline(ctx, "main", pc)
				require.NoError(t, err)
				assert.True(t, pp.Public)
			})

			t.Run("TeamMemberRole", func(t *testing.T) {
				_, err := svc.CreateUser(ctx, user.User{
					FullName: "member", Username: "member",
					Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
				}, false)
				require.NoError(t, err)
				_, err = svc.CreateTeamMember(ctx, "main", team.Member{Role: role.Read, User: user.User{Username: "member"}})
				require.NoError(t, err)

				m, err := svc.UpdateTeamMember(ctx, "main", "member", team.Member{Role: role.Write})
				require.NoError(t, err)
				assert.Equal(t, role.Write, m.Role)
			})
		})
	}
}
