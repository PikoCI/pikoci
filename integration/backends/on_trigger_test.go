//go:build integration

package backends_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/handlers"
	"github.com/soheilhy/cmux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	pikogrpc "github.com/pikoci/pikoci/pikoci/grpc"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/resource"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/transport/http/client"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// onTriggerTestPEM stands in for the GitHub App private key of #707: a
// multi-line .pem read through the built-in pikoci://file secret type.
const onTriggerTestPEM = "-----BEGIN TEST KEY-----\nMIIEpAIBAAKCAQEAon7rigger\n-----END TEST KEY-----"

// runOnTriggerSecretScenario is the end-to-end check for #707: an on_trigger
// notify hook whose param is backed by a pikoci://file secret must receive the
// resolved value, both when a resource check finds a new version and when a
// user manually re-triggers a version. The job's task fails unless the hook
// has already written its sentinel, so a passing build also proves the hook
// ran before the build was created.
func runOnTriggerSecretScenario(t *testing.T, ctx context.Context, svc pikoci.Service, pc string) {
	t.Helper()

	dir := t.TempDir()
	pemFile := filepath.Join(dir, "github_app.pem")
	require.NoError(t, os.WriteFile(pemFile, []byte(onTriggerTestPEM+"\n"), 0600))
	keyOut := filepath.Join(dir, "hook-key.out")
	metaOut := filepath.Join(dir, "hook-meta.out")

	hcl := []byte(fmt.Sprintf(`
resource_type "tick" {
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '[{\"ref\":\"abc123\"}]'"]
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

secret_type "pem" {
  source = "pikoci://file"
  path   = %q
}

variable "github_pem" {
  type = string
  secret "pem" {
    key = "content"
  }
}

notification_type "recorder" {
  params = ["private_key"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "printf '%%s' \"$param_private_key\" > %s; printf '%%s|%%s|%%s|%%s' \"$version_ref\" \"$BUILD_JOB_NAME\" \"$BUILD_NUMBER\" \"$notify_status\" > %s"]
  }
}

notification "recorder" "ci" {
  params {
    private_key = var.github_pem
  }
}

job "build" {
  on_trigger {
    notify "recorder" "ci" {
      status = "queued"
    }
  }
  get "tick" "repo" {
    trigger = true
  }
  task "hook-ran-first" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "test -f %s"]
    }
  }
}
`, pemFile, keyOut, metaOut, keyOut))

	_, err := svc.CreatePipeline(ctx, "main", pc, hcl, nil)
	require.NoError(t, err)

	assertHookGotSecret := func(t *testing.T) {
		t.Helper()
		got, err := os.ReadFile(keyOut)
		require.NoError(t, err, "on_trigger hook must have written its sentinel")
		assert.NotContains(t, string(got), "__pikoci_secret", "hook received an unresolved secret placeholder")
		assert.Equal(t, onTriggerTestPEM, strings.TrimSpace(string(got)), "hook must receive the resolved PEM")

		meta, err := os.ReadFile(metaOut)
		require.NoError(t, err)
		assert.Equal(t, "abc123|build||queued", string(meta), "version_ref|BUILD_JOB_NAME|BUILD_NUMBER|notify_status")
	}

	waitBuilds := func(t *testing.T, n int) []*build.Build {
		t.Helper()
		var builds []*build.Build
		require.Eventually(t, func() bool {
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
		}, 20*time.Second, 200*time.Millisecond, "expected %d finished builds", n)
		require.Len(t, builds, n)
		return builds
	}

	t.Run("ResourceCheck", func(t *testing.T) {
		require.NoError(t, svc.TriggerPipelineResource(ctx, "main", pc, "tick.repo"))

		builds := waitBuilds(t, 1)
		assert.Equal(t, build.Succeeded, builds[0].Status, "build must succeed (task fails if the hook did not run first), error: %s", builds[0].Error)
		assertHookGotSecret(t)
	})

	t.Run("ManualRetrigger", func(t *testing.T) {
		require.NoError(t, os.Remove(keyOut))
		require.NoError(t, os.Remove(metaOut))

		vers, _, err := svc.ListResourceVersions(ctx, "main", pc, "tick.repo", nil, nil, 0)
		require.NoError(t, err)
		require.Len(t, vers, 1)
		target := vers[0].ID

		// A newer version exists, so re-triggering the older one proves the
		// worker looks the requested version up instead of taking the latest.
		_, err = svc.CreateResourceVersion(ctx, "main", pc, "tick.repo", resource.Version{
			Version: map[string]interface{}{"ref": "zzz999"},
		})
		require.NoError(t, err)

		require.NoError(t, svc.TriggerResourceVersion(ctx, "main", pc, "tick.repo", target))

		builds := waitBuilds(t, 2)
		for _, b := range builds {
			assert.Equal(t, build.Succeeded, b.Status, "build %s must succeed, error: %s", b.BuildNumber, b.Error)
		}
		assert.Equal(t, target, builds[0].VersionID, "the re-triggered build must use the requested version")
		assertHookGotSecret(t) // also asserts version_ref=abc123, the requested version
	})
}

// TestOnTrigger_SecretParam_EmbeddedWorker runs the #707 scenario with the
// worker embedded in the server process, on every configured DB backend.
func TestOnTrigger_SecretParam_EmbeddedWorker(t *testing.T) {
	for _, system := range dbSystems() {
		t.Run(system, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "on-trigger-embedded", "db", system)

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

			w := worker.New(svc, logger.With("component", "worker"), "test-worker", "test", "", 1, nil, false)
			go w.Run(ctx)

			runOnTriggerSecretScenario(t, ctx, svc, "on-trigger-embedded")
		})
	}
}

// TestOnTrigger_SecretParam_GRPCWorker runs the #707 scenario with a
// standalone worker connected over gRPC, the transport every remote worker
// in production uses.
func TestOnTrigger_SecretParam_GRPCWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "on-trigger-grpc")

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

	tr := mysql.NewTeamRepository(db)
	jwtSecret := []byte("test-secret")
	wn := notifier.New()
	svc := pikoci.New(ctx,
		mysql.NewUserRepository(db), tr, mysql.NewPipelineRepository(db),
		mysql.NewJobRepository(db), mysql.NewResourceRepository(db, mysql.SQLite), mysql.NewResourceTypeRepository(db),
		mysql.NewBuildRepository(db, mysql.SQLite), mysql.NewRunnerRepository(db), mysql.NewSecretTypeRepository(db),
		mysql.NewTriggerRepository(db), mysql.NewWorkerRepository(db, mysql.SQLite), mysql.NewApiTokenRepository(db), nil, nil,
		unitwork.NewStartUnitOfWork(db, mysql.SQLite), jwtSecret, wn, logger)
	svc.StartScheduler(ctx)
	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin", Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	streamMgr := pikogrpc.NewWorkerStreamManager()
	grpcServer := pikogrpc.NewServer(svc, wn, streamMgr, jwtSecret, tr, logger.With("component", "gRPC"))
	grpcSrv := grpc.NewServer()
	workerv1.RegisterWorkerServiceServer(grpcSrv, grpcServer)
	svc.GRPCServer = grpcServer

	httpHandler := tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, mysql.SQLite, "test", "test", "", nil)
	httpSrv := &http.Server{Handler: handlers.CombinedLoggingHandler(os.Stderr, httpHandler)}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()

	m := cmux.New(lis)
	grpcLis := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	httpLis := m.Match(cmux.Any())
	go grpcSrv.Serve(grpcLis)
	go httpSrv.Serve(httpLis)
	go m.Serve()
	t.Cleanup(func() {
		grpcSrv.Stop()
		httpSrv.Close()
	})

	workerToken := generateTestWorkerJWT(jwtSecret)
	httpClient, err := client.New(fmt.Sprintf("http://%s", addr), workerToken)
	require.NoError(t, err)
	grpcConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { grpcConn.Close() })

	w := worker.NewGRPC(httpClient, workerv1.NewWorkerServiceClient(grpcConn), logger.With("worker", "grpc-worker"), "grpc-worker", "test", "", 1, workerToken, addr, nil, false)
	go w.Run(ctx)
	require.Eventually(t, func() bool {
		return streamMgr.ConnectedCount() > 0
	}, 3*time.Second, 50*time.Millisecond, "worker should connect via gRPC")

	runOnTriggerSecretScenario(t, ctx, svc, "on-trigger-grpc")
}
