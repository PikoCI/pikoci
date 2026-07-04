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
	"github.com/pikoci/pikoci/pikoci/team"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/transport/http/client"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/pikoci/pikoci/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newTeamIsolationTestService(t *testing.T, ctx context.Context, logger *slog.Logger) *pikoci.PikoCI {
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

	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, nil, nil, suow, []byte("test"), notifier.New(), logger)
	svc.StartScheduler(ctx)

	_, _ = svc.CreateUser(ctx, user.User{
		FullName: "admin", Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	return svc
}

func TestTeamWorkerOnlyGetsTeamWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := newTeamIsolationTestService(t, ctx, logger)

	// Create two teams with simple pipelines
	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamA"})
	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamB"})

	pipeA := []byte(`job "build" {}`)
	pipeB := []byte(`job "build" {}`)
	svc.CreatePipeline(ctx, "teama", "pipeA", pipeA, nil)
	svc.CreatePipeline(ctx, "teamb", "pipeB", pipeB, nil)

	// Create pending builds on both teams
	svc.CreateJobBuild(ctx, "teama", "pipeA", "build", build.Build{})
	svc.CreateJobBuild(ctx, "teamb", "pipeB", "build", build.Build{})

	// Team worker for teamA should only get teamA's work
	wc := workitem.WorkerContext{TeamCanonical: "teama"}
	item, err := svc.NextWork(ctx, wc)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "teama", item.Body.TeamCanonical)
	assert.Equal(t, "pipeA", item.Body.PipelineCanonical)

	// teamA worker should not get teamB's work (no more teamA work)
	item, err = svc.NextWork(ctx, wc)
	require.NoError(t, err)
	assert.Nil(t, item)
}

func TestGlobalWorkerDefersToTeamWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := newTeamIsolationTestService(t, ctx, logger)

	// Create team with pipeline
	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamA"})
	svc.CreatePipeline(ctx, "teama", "pipe", []byte(`job "build" {}`), nil)
	svc.CreateJobBuild(ctx, "teama", "pipe", "build", build.Build{})

	// Set up a stream manager with a team worker for teamA
	streamMgr := pikogrpc.NewWorkerStreamManager()
	teamWs := pikogrpc.NewWorkerStream("team-worker", 1, nil, false, "teama")
	streamMgr.Register(teamWs)
	svc.TeamWorkerChecker = streamMgr

	// Global worker should skip teamA's work because team worker exists
	globalWc := workitem.WorkerContext{TeamCanonical: ""}
	item, err := svc.NextWork(ctx, globalWc)
	require.NoError(t, err)
	assert.Nil(t, item, "global worker should defer to team worker")
}

func TestGlobalWorkerServesTeamWithoutDedicatedWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := newTeamIsolationTestService(t, ctx, logger)

	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamA"})
	svc.CreatePipeline(ctx, "teama", "pipe", []byte(`job "build" {}`), nil)
	svc.CreateJobBuild(ctx, "teama", "pipe", "build", build.Build{})

	// No team workers → global worker should serve
	streamMgr := pikogrpc.NewWorkerStreamManager()
	svc.TeamWorkerChecker = streamMgr

	globalWc := workitem.WorkerContext{TeamCanonical: ""}
	item, err := svc.NextWork(ctx, globalWc)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "teama", item.Body.TeamCanonical)
}

func TestTeamWorkerCannotGetOtherTeamWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := newTeamIsolationTestService(t, ctx, logger)

	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamA"})
	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamB"})
	svc.CreatePipeline(ctx, "teamb", "pipe-b", []byte(`job "build" {}`), nil)
	svc.CreateJobBuild(ctx, "teamb", "pipe-b", "build", build.Build{})

	// Team A worker should never get Team B's work, even when Team B
	// has no dedicated workers
	wc := workitem.WorkerContext{TeamCanonical: "teama"}
	item, err := svc.NextWork(ctx, wc)
	require.NoError(t, err)
	assert.Nil(t, item, "team A worker must not get team B's work")
}

func TestRegeneratedTokenInvalidatesOldSalt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := newTeamIsolationTestService(t, ctx, logger)

	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamA"})

	// Generate initial token
	token1, err := svc.GenerateTeamWorkerToken(ctx, "teama")
	require.NoError(t, err)
	require.NotEmpty(t, token1)

	// Regenerate → new token with new salt
	token2, err := svc.GenerateTeamWorkerToken(ctx, "teama")
	require.NoError(t, err)
	require.NotEmpty(t, token2)
	assert.NotEqual(t, token1, token2, "regenerated token should differ")

	// GetTeamWorkerToken returns a token signed with the current salt.
	// The old token (token1) has a different salt baked in, so it won't
	// match the current one — proving the old salt is invalidated.
	currentToken, err := svc.GetTeamWorkerToken(ctx, "teama")
	require.NoError(t, err)
	assert.Equal(t, token2, currentToken, "current token should match the regenerated one")
	assert.NotEqual(t, token1, currentToken, "old token should not match current")

	// Validate old token against gRPC server → should be rejected
	n := notifier.New()
	sm := pikogrpc.NewWorkerStreamManager()
	grpcSrv := pikogrpc.NewServer(nil, n, sm, svc.JWTSecret, svc.Teams, logger)
	resp, err := grpcSrv.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "old-worker",
		WorkerToken: token1,
		MaxJobs:     1,
	})
	require.NoError(t, err)
	assert.False(t, resp.Accepted, "old token should be rejected after regeneration")

	// New token should be accepted
	resp, err = grpcSrv.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "new-worker",
		WorkerToken: token2,
		MaxJobs:     1,
	})
	require.NoError(t, err)
	assert.True(t, resp.Accepted, "new token should be accepted")
}

func TestTeamWorkerWithTagsCompose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := newTeamIsolationTestService(t, ctx, logger)

	svc.CreateTeam(ctx, "admin", team.Team{Name: "teamA"})

	hcl := []byte(`
job "gpu-job" {
  tags = ["gpu"]
  task "work" {
    run "exec" {
      path = "/bin/true"
    }
  }
}
job "cpu-job" {
  task "work" {
    run "exec" {
      path = "/bin/true"
    }
  }
}
`)
	_, err := svc.CreatePipeline(ctx, "teama", "pipe", hcl, nil)
	require.NoError(t, err)

	svc.CreateJobBuild(ctx, "teama", "pipe", "gpu-job", build.Build{})
	svc.CreateJobBuild(ctx, "teama", "pipe", "cpu-job", build.Build{})

	// Team worker with gpu tag should only get gpu-job from its team
	wc := workitem.WorkerContext{
		TeamCanonical: "teama",
		Tags:          []string{"gpu"},
		ExclusiveTags: true,
	}
	item, err := svc.NextWork(ctx, wc)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "gpu-job", item.Body.JobName)
	assert.Equal(t, "teama", item.Body.TeamCanonical)

	// Same worker should NOT get cpu-job (exclusive tags)
	item, err = svc.NextWork(ctx, wc)
	require.NoError(t, err)
	assert.Nil(t, item, "exclusive gpu worker should not get untagged cpu-job")
}

// TestTeamIsolation_FullGRPCFlow is an end-to-end test that starts a real
// server (HTTP + gRPC via cmux), connects a team-scoped worker and a global
// worker, triggers builds on two teams, and verifies:
//   - The team worker only processes its own team's builds
//   - The global worker processes the other team's builds
//   - Workers show correct team_canonical in the DB
func TestTeamIsolation_FullGRPCFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("test", "team-isolation-e2e")

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

	svc.CreateUser(ctx, user.User{
		FullName: "admin", Username: "admin",
		Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm",
	}, true)

	// --- Start HTTP + gRPC server via cmux ---
	streamMgr := pikogrpc.NewWorkerStreamManager()
	grpcServer := pikogrpc.NewServer(svc, wn, streamMgr, jwtSecret, tr, logger.With("component", "gRPC"))
	grpcSrv := grpc.NewServer()
	workerv1.RegisterWorkerServiceServer(grpcSrv, grpcServer)
	svc.GRPCServer = grpcServer
	svc.TeamWorkerChecker = streamMgr

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

	// --- Create two teams with simple pipelines ---
	svc.CreateTeam(ctx, "admin", team.Team{Name: "alpha"})
	svc.CreateTeam(ctx, "admin", team.Team{Name: "beta"})

	hcl := []byte(`
resource "cron" "trigger" {
  check_interval = "@every 1h"
}
job "test-job" {
  get "cron" "trigger" { trigger = true }
  task "work" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo hello"]
    }
  }
}
`)
	_, err = svc.CreatePipeline(ctx, "alpha", "alpha-pipe", hcl, nil)
	require.NoError(t, err)
	_, err = svc.CreatePipeline(ctx, "beta", "beta-pipe", hcl, nil)
	require.NoError(t, err)

	// Seed resource versions
	for _, tc := range []string{"alpha", "beta"} {
		pipe := tc + "-pipe"
		svc.CreateResourceVersion(ctx, tc, pipe, "cron.trigger", resource.Version{
			Version: map[string]interface{}{"date": "seed"},
		})
		svc.CreateResourceVersion(ctx, tc, pipe, "cron.trigger", resource.Version{
			Version: map[string]interface{}{"date": "v2"},
		})
	}

	// --- Generate team worker token for alpha ---
	teamToken, err := svc.GenerateTeamWorkerToken(ctx, "alpha")
	require.NoError(t, err)

	// Global worker token
	globalToken := generateTestWorkerJWT(jwtSecret)

	// --- Start team worker for alpha ---
	teamHTTPClient, err := client.New(fmt.Sprintf("http://%s", addr), teamToken)
	require.NoError(t, err)
	teamGRPCConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { teamGRPCConn.Close() })
	teamGRPCClient := workerv1.NewWorkerServiceClient(teamGRPCConn)
	teamWorker := worker.NewGRPC(teamHTTPClient, teamGRPCClient, logger.With("worker", "team-alpha"),
		"team-alpha-worker", "test", "", 1, teamToken, addr, nil, false)
	go teamWorker.Run(ctx)

	// --- Start global worker (with its own cancellable context) ---
	globalCtx, globalCancel := context.WithCancel(ctx)
	globalHTTPClient, err := client.New(fmt.Sprintf("http://%s", addr), globalToken)
	require.NoError(t, err)
	globalGRPCConn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { globalGRPCConn.Close() })
	globalGRPCClient := workerv1.NewWorkerServiceClient(globalGRPCConn)
	globalWorker := worker.NewGRPC(globalHTTPClient, globalGRPCClient, logger.With("worker", "global"),
		"global-worker", "test", "", 1, globalToken, addr, nil, false)
	go globalWorker.Run(globalCtx)

	// Wait for both workers to connect
	require.Eventually(t, func() bool {
		return streamMgr.ConnectedCount() >= 2
	}, 5*time.Second, 50*time.Millisecond, "both workers should connect")

	// Verify team worker stream has correct team canonical
	require.True(t, streamMgr.HasTeamWorkers("alpha"), "stream manager should know alpha has team workers")
	require.False(t, streamMgr.HasTeamWorkers("beta"), "beta should not have team workers")

	// --- Trigger builds on both teams ---
	err = svc.TriggerPipelineJob(ctx, "alpha", "alpha-pipe", "test-job")
	require.NoError(t, err)
	err = svc.TriggerPipelineJob(ctx, "beta", "beta-pipe", "test-job")
	require.NoError(t, err)

	// --- Wait for both builds to complete ---
	require.Eventually(t, func() bool {
		alphaBuilds, _, _ := svc.ListJobBuilds(ctx, "alpha", "alpha-pipe", "test-job", nil, nil, 0)
		betaBuilds, _, _ := svc.ListJobBuilds(ctx, "beta", "beta-pipe", "test-job", nil, nil, 0)
		if len(alphaBuilds) == 0 || len(betaBuilds) == 0 {
			return false
		}
		return alphaBuilds[0].Status == build.Succeeded && betaBuilds[0].Status == build.Succeeded
	}, 15*time.Second, 200*time.Millisecond, "both builds should succeed")

	// --- Verify worker DB records show correct team_canonical ---
	workers, err := svc.ListWorkers(ctx)
	require.NoError(t, err)

	for _, wk := range workers {
		switch wk.Name {
		case "team-alpha-worker":
			assert.Equal(t, "alpha", wk.TeamCanonical, "team worker should have team_canonical=alpha in DB")
		case "global-worker":
			assert.Empty(t, wk.TeamCanonical, "global worker should have empty team_canonical in DB")
		}
	}

	// ===================================================================
	// Phase 2: Kill global worker → beta builds should stay pending
	// because the team-alpha worker must NOT pick up beta's work.
	// ===================================================================

	globalCancel()

	// Wait for global worker to disconnect from the stream manager
	require.Eventually(t, func() bool {
		return streamMgr.ConnectedCount() == 1
	}, 5*time.Second, 50*time.Millisecond, "global worker should disconnect")

	// Trigger a new build on beta
	err = svc.TriggerPipelineJob(ctx, "beta", "beta-pipe", "test-job")
	require.NoError(t, err)

	// Wait a bit and verify the beta build stays pending — the alpha
	// team worker must NOT pick it up.
	time.Sleep(2 * time.Second)

	betaBuilds, _, err := svc.ListJobBuilds(ctx, "beta", "beta-pipe", "test-job", nil, nil, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(betaBuilds), 2, "should have at least 2 beta builds")

	// The newest build (index 0) should still be pending
	assert.Equal(t, build.Pending, betaBuilds[0].Status,
		"beta build should stay pending: team-alpha worker must not process it")

	// ===================================================================
	// Phase 3: Trigger an alpha build → team worker should still pick it up
	// ===================================================================

	err = svc.TriggerPipelineJob(ctx, "alpha", "alpha-pipe", "test-job")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		alphaBuilds, _, _ := svc.ListJobBuilds(ctx, "alpha", "alpha-pipe", "test-job", nil, nil, 0)
		if len(alphaBuilds) < 2 {
			return false
		}
		return alphaBuilds[0].Status == build.Succeeded
	}, 10*time.Second, 200*time.Millisecond,
		"alpha build should succeed even without global worker")

	// Re-check: beta build is STILL pending
	betaBuilds, _, err = svc.ListJobBuilds(ctx, "beta", "beta-pipe", "test-job", nil, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, build.Pending, betaBuilds[0].Status,
		"beta build must remain pending with only alpha team worker online")

	// ===================================================================
	// Phase 4: Start a new global worker → picks up the pending beta build
	// ===================================================================

	globalCtx2, globalCancel2 := context.WithCancel(ctx)
	defer globalCancel2()
	globalHTTPClient2, err := client.New(fmt.Sprintf("http://%s", addr), globalToken)
	require.NoError(t, err)
	globalGRPCConn2, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { globalGRPCConn2.Close() })
	globalGRPCClient2 := workerv1.NewWorkerServiceClient(globalGRPCConn2)
	globalWorker2 := worker.NewGRPC(globalHTTPClient2, globalGRPCClient2, logger.With("worker", "global2"),
		"global-worker-2", "test", "", 1, globalToken, addr, nil, false)
	go globalWorker2.Run(globalCtx2)

	// Wait for new global worker to connect
	require.Eventually(t, func() bool {
		return streamMgr.ConnectedCount() >= 2
	}, 5*time.Second, 50*time.Millisecond, "new global worker should connect")

	// The pending beta build should now be picked up by the new global worker
	require.Eventually(t, func() bool {
		betaBuilds, _, _ := svc.ListJobBuilds(ctx, "beta", "beta-pipe", "test-job", nil, nil, 0)
		if len(betaBuilds) < 2 {
			return false
		}
		return betaBuilds[0].Status == build.Succeeded
	}, 10*time.Second, 200*time.Millisecond,
		"pending beta build should complete after new global worker connects")
}
