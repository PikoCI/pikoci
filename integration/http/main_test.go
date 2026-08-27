//go:build integration

package http_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/notifier"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
)

var (
	pikoURL   string
	jwtSecret = []byte("test-secret")
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).With("service", "pikoci")

	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})
	if err != nil {
		panic(err)
	}

	err = migrate.Migrate(db, mysql.Mem)
	if err != nil {
		panic(err)
	}

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
	wr := mysql.NewWorkerRepository(db, mysql.Mem)
	atr := mysql.NewApiTokenRepository(db)
	suow := unitwork.NewStartUnitOfWork(db, mysql.Mem)
	wn := notifier.New()

	alr := mysql.NewAuditLogRepository(db)
	var svc = pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, wr, atr, alr, nil, suow, jwtSecret, wn, logger)
	svc.StartScheduler(ctx)

	// Wire the configuration store with a master key, so the secret paths are
	// exercised rather than short-circuiting as unconfigured.
	svc.EnableSecretStore(mysql.NewSecretRepository(db), "integration-master-key")

	var handler = tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, mysql.Mem, "test", "abc1234", "", nil)
	server := httptest.NewServer(handler)
	pikoURL = server.URL
	defer server.Close()

	// Create the default admin user with a known password hash
	isHash := true
	// admin123
	_, _ = svc.CreateUser(ctx, user.User{FullName: "Admin", Username: "admin", Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm", Admin: true}, isHash)

	// Start a background worker
	go func() {
		runWorker(ctx, svc, 1, "INFO")
	}()

	return m.Run()
}

func runWorker(ctx context.Context, s pikoci.Service, c int, llvl string) error {
	var lvl slog.Level
	switch llvl {
	case "DEBUG":
		lvl = slog.LevelDebug
	case "WARN":
		lvl = slog.LevelWarn
	case "ERROR":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})).With("service", "worker")

	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		nlogger := logger.With("num", i+1)
		nlogger.Info(fmt.Sprintf("Starting Worker %d", i+1))
		w := worker.New(s, nlogger, fmt.Sprintf("test-worker-%d", i+1), "test", "", c, nil, false)

		go func() {
			err := w.Run(ctx)
			if err != nil {
				logger.Error("failed to Run worker", "error", err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
}
