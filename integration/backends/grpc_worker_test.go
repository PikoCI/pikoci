//go:build integration

package backends_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

func generateTestWorkerJWT(secret []byte) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	s, _ := token.SignedString(secret)
	return s
}

// TestGRPCWorkerFullFlow spins up a real server (HTTP + gRPC via cmux),
// connects a standalone worker via gRPC, triggers a build, and verifies
// the build completes with steps persisted.
func TestGRPCWorkerFullFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "grpc-flow")

	// --- Set up DB + service ---
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
	wr := mysql.NewWorkerRepository(db, mysql.SQLite)
	atr := mysql.NewApiTokenRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.SQLite)

	jwtSecret := []byte("test-secret")
	wn := notifier.New()
	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, wr, atr, nil, suow, jwtSecret, wn, logger)
	svc.StartScheduler(ctx)

	// Create admin user (ignore error if already exists from migrations)
	svc.CreateUser(ctx, user.User{
		FullName: "admin",
		Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	// --- Start HTTP + gRPC server via cmux ---
	streamMgr := pikogrpc.NewWorkerStreamManager()
	grpcServer := pikogrpc.NewServer(svc, wn, streamMgr, jwtSecret, tr, logger.With("component", "gRPC"))
	grpcSrv := grpc.NewServer()
	workerv1.RegisterWorkerServiceServer(grpcSrv, grpcServer)
	svc.GRPCServer = grpcServer

	httpHandler := tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, mysql.SQLite, "test", "test")
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

	// --- Generate worker JWT ---
	workerToken := generateTestWorkerJWT(jwtSecret)

	// --- Create HTTP client (for data queries) ---
	httpClient, err := client.New(fmt.Sprintf("http://%s", addr), workerToken)
	require.NoError(t, err)

	// --- Create gRPC client ---
	grpcConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { grpcConn.Close() })
	grpcClient := workerv1.NewWorkerServiceClient(grpcConn)

	// --- Start standalone worker via gRPC ---
	w := worker.NewGRPC(httpClient, grpcClient, logger.With("worker", "test-worker-1"), "test-worker-1", "test", "", 1, workerToken, addr, nil, false)
	go w.Run(ctx)

	// Wait for worker to register
	require.Eventually(t, func() bool {
		return streamMgr.ConnectedCount() > 0
	}, 3*time.Second, 50*time.Millisecond, "worker should connect via gRPC")

	// --- Create pipeline with a simple job ---
	hclConfig := []byte(`
resource "cron" "trigger" {
  check_interval = "@every 1h"
}

job "grpc-test" {
  get "cron" "trigger" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo hello from gRPC worker"]
    }
  }
}
`)
	pp, err := svc.CreatePipeline(ctx, "main", "grpc-test", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	// Seed a version so the next one triggers jobs
	_, err = svc.CreateResourceVersion(ctx, "main", "grpc-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)

	// Create a second version to trigger the job
	_, err = svc.CreateResourceVersion(ctx, "main", "grpc-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "v2"},
	})
	require.NoError(t, err)

	// Trigger the job (creates a pending build + notifies workers)
	err = svc.TriggerPipelineJob(ctx, "main", "grpc-test", "grpc-test")
	require.NoError(t, err)

	// --- Wait for build to complete ---
	require.Eventually(t, func() bool {
		builds, _, err := svc.ListJobBuilds(ctx, "main", "grpc-test", "grpc-test", nil, nil, 0)
		if err != nil || len(builds) == 0 {
			return false
		}
		return builds[0].Status != build.Started && builds[0].Status != build.Pending
	}, 15*time.Second, 200*time.Millisecond, "build should complete")

	// --- Verify build succeeded with steps ---
	builds, _, err := svc.ListJobBuilds(ctx, "main", "grpc-test", "grpc-test", nil, nil, 0)
	require.NoError(t, err)
	require.NotEmpty(t, builds)

	b := builds[0]
	assert.Equal(t, build.Succeeded, b.Status, "build should succeed, error: %s", b.Error)
	assert.NotEmpty(t, b.Steps, "build should have steps persisted")

	// Verify the worker shows up in the DB with metadata
	workers, err := svc.ListWorkers(ctx)
	require.NoError(t, err)
	found := false
	for _, wk := range workers {
		if wk.Name == "test-worker-1" {
			found = true
			assert.NotEmpty(t, wk.Hostname, "worker should have hostname from heartbeat")
			assert.NotEmpty(t, wk.OS, "worker should have OS from heartbeat")
			assert.NotEmpty(t, wk.Arch, "worker should have arch from heartbeat")
			break
		}
	}
	assert.True(t, found, "worker should be registered via heartbeat")
}

// TestStartupRecoveryFailsOrphanedBuilds verifies that RecoverOrphanedBuilds
// transitions "started" builds to "failed" without affecting other statuses.
func TestStartupRecoveryFailsOrphanedBuilds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "startup-recovery")

	// --- Set up DB + service ---
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
	wr := mysql.NewWorkerRepository(db, mysql.SQLite)
	atr := mysql.NewApiTokenRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.SQLite)

	jwtSecret := []byte("test-secret")
	wn := notifier.New()
	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, wr, atr, nil, suow, jwtSecret, wn, logger)

	// Create a pipeline + job so we can create builds
	hclConfig := []byte(`
resource "cron" "trigger" {
  check_interval = "@every 1h"
}

job "recovery-test" {
  get "cron" "trigger" {
    trigger = true
  }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo hello"]
    }
  }
}
`)
	_, err = svc.CreatePipeline(ctx, "main", "recovery-test", hclConfig, nil)
	require.NoError(t, err)

	// Seed a resource version
	_, err = svc.CreateResourceVersion(ctx, "main", "recovery-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)

	// Create builds in various statuses
	pendingBuild, err := svc.CreateJobBuild(ctx, "main", "recovery-test", "recovery-test", build.Build{})
	require.NoError(t, err)

	startedBuild, err := svc.CreateJobBuild(ctx, "main", "recovery-test", "recovery-test", build.Build{})
	require.NoError(t, err)
	// Transition to started via StartPending
	_, err = svc.StartPendingBuild(ctx, "main", "recovery-test", "recovery-test", startedBuild.ID)
	require.NoError(t, err)

	succeededBuild, err := svc.CreateJobBuild(ctx, "main", "recovery-test", "recovery-test", build.Build{})
	require.NoError(t, err)
	_, err = svc.StartPendingBuild(ctx, "main", "recovery-test", "recovery-test", succeededBuild.ID)
	require.NoError(t, err)
	err = svc.UpdateJobBuild(ctx, "main", "recovery-test", "recovery-test", succeededBuild.BuildNumber, build.Build{
		Status:    build.Succeeded,
		StartedAt: time.Now(),
	})
	require.NoError(t, err)

	// Verify started count before recovery
	count, err := svc.CountStartedBuilds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Run recovery
	n, err := svc.RecoverOrphanedBuilds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "should have recovered exactly 1 orphaned build")

	// Verify no started builds remain
	count, err = svc.CountStartedBuilds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify the started build is now failed with the right message
	b, err := svc.GetJobBuild(ctx, "main", "recovery-test", "recovery-test", startedBuild.BuildNumber)
	require.NoError(t, err)
	assert.Equal(t, build.Failed, b.Status)
	assert.Contains(t, b.Error, "server shutdown")

	// Verify pending build is still pending
	b, err = svc.GetJobBuild(ctx, "main", "recovery-test", "recovery-test", pendingBuild.BuildNumber)
	require.NoError(t, err)
	assert.Equal(t, build.Pending, b.Status)

	// Verify succeeded build is still succeeded
	b, err = svc.GetJobBuild(ctx, "main", "recovery-test", "recovery-test", succeededBuild.BuildNumber)
	require.NoError(t, err)
	assert.Equal(t, build.Succeeded, b.Status)
}

// TestServerDrainWaitsForSeparatedWorkerBuilds verifies that SIGQUIT-style
// drain waits for started builds to reach a terminal status before shutting down.
func TestServerDrainWaitsForSeparatedWorkerBuilds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "drain-wait")

	// --- Set up DB + service ---
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
	wr := mysql.NewWorkerRepository(db, mysql.SQLite)
	atr := mysql.NewApiTokenRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.SQLite)

	jwtSecret := []byte("test-secret")
	wn := notifier.New()
	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, wr, atr, nil, suow, jwtSecret, wn, logger)
	svc.StartScheduler(ctx)

	// Create admin user
	svc.CreateUser(ctx, user.User{
		FullName: "admin",
		Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	// --- Start HTTP + gRPC server ---
	streamMgr := pikogrpc.NewWorkerStreamManager()
	grpcServer := pikogrpc.NewServer(svc, wn, streamMgr, jwtSecret, tr, logger.With("component", "gRPC"))
	grpcSrv := grpc.NewServer()
	workerv1.RegisterWorkerServiceServer(grpcSrv, grpcServer)
	svc.GRPCServer = grpcServer

	httpHandler := tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, mysql.SQLite, "test", "test")
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
	grpcClient := workerv1.NewWorkerServiceClient(grpcConn)

	// Start standalone worker with a slow job
	w := worker.NewGRPC(httpClient, grpcClient, logger.With("worker", "drain-worker-1"), "drain-worker-1", "test", "", 1, workerToken, addr, nil, false)
	go w.Run(ctx)

	require.Eventually(t, func() bool {
		return streamMgr.ConnectedCount() > 0
	}, 3*time.Second, 50*time.Millisecond, "worker should connect via gRPC")

	// Create pipeline with a slow job (sleeps 3 seconds)
	hclConfig := []byte(`
resource "cron" "trigger" {
  check_interval = "@every 1h"
}

job "drain-test" {
  get "cron" "trigger" {
    trigger = true
  }
  task "slow-work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "sleep 3 && echo done"]
    }
  }
}
`)
	_, err = svc.CreatePipeline(ctx, "main", "drain-test", hclConfig, nil)
	require.NoError(t, err)

	// Seed + trigger
	_, err = svc.CreateResourceVersion(ctx, "main", "drain-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "seed"},
	})
	require.NoError(t, err)
	_, err = svc.CreateResourceVersion(ctx, "main", "drain-test", "cron.trigger", resource.Version{
		Version: map[string]interface{}{"date": "v2"},
	})
	require.NoError(t, err)
	err = svc.TriggerPipelineJob(ctx, "main", "drain-test", "drain-test")
	require.NoError(t, err)

	// Wait until the build is started
	require.Eventually(t, func() bool {
		builds, _, err := svc.ListJobBuilds(ctx, "main", "drain-test", "drain-test", nil, nil, 0)
		if err != nil || len(builds) == 0 {
			return false
		}
		return builds[0].Status == build.Started
	}, 10*time.Second, 200*time.Millisecond, "build should reach started status")

	// Now simulate drain: poll CountStartedBuilds until 0
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		deadline := time.After(15 * time.Second)
		for {
			n, err := svc.CountStartedBuilds(ctx)
			if err != nil || n == 0 {
				return
			}
			select {
			case <-time.After(500 * time.Millisecond):
			case <-deadline:
				return
			}
		}
	}()

	// Drain should complete within the build's runtime (~3s) + buffer
	select {
	case <-drainDone:
		// Drain completed - build should be in terminal status
	case <-time.After(15 * time.Second):
		t.Fatal("drain did not complete in time")
	}

	// Verify build reached terminal status
	builds, _, err := svc.ListJobBuilds(ctx, "main", "drain-test", "drain-test", nil, nil, 0)
	require.NoError(t, err)
	require.NotEmpty(t, builds)
	b := builds[0]
	assert.Equal(t, build.Succeeded, b.Status, "build should have succeeded, got: %s (error: %s)", b.Status, b.Error)
}
