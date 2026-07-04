//go:build integration

package backends_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	pikogrpc "github.com/pikoci/pikoci/pikoci/grpc"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/workitem"
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
