//go:build integration

package selenium_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/pikoci/pikoci/worker"
)

var pikoURL string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	// Use bcrypt cost 4 (MinCost) throughout all integration tests so that
	// password hashing and verification complete in milliseconds rather than
	// several seconds. Cost 14 (production default) on a loaded arm64 server
	// exceeds the 5-second Selenium waitFor timeouts.
	utils.BcryptCost = bcrypt.MinCost

	jwtSecret := []byte("secret")
	ctx := context.Background()

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
	suow := unitwork.NewStartUnitOfWork(db, mysql.Mem)
	wn := notifier.New()
	alr := mysql.NewAuditLogRepository(db)
	opr := mysql.NewOAuthProviderRepository(db)
	var svc = pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, nil, alr, opr, suow, jwtSecret, wn, logger)
	// The secret store is opt-in, so the UI tests need it wired up explicitly.
	svc.EnableSecretStore(mysql.NewSecretRepository(db), "integration-master-key")
	svc.StartScheduler(ctx)

	oauthStateStore := pikoci.NewOAuthStateStore(ctx)

	// Create a placeholder handler that will be replaced once we know the external URL.
	// We use a mux so we can swap the inner handler after getting the server URL.
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	pikoURL = server.URL
	defer server.Close()

	// Now build the real handler with the correct external URL for OAuth callbacks
	var handler = tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, mysql.Mem, "test", "abc1234", pikoURL, oauthStateStore)
	mux.Handle("/", handler)

	pepitoPwd, _ := bcrypt.GenerateFromPassword([]byte("pepito"), bcrypt.MinCost)
	grilloPwd, _ := bcrypt.GenerateFromPassword([]byte("grillo"), bcrypt.MinCost)
	isHash := true
	_, _ = svc.CreateUser(ctx, user.User{FullName: "pepito", Username: "pepito", Password: string(pepitoPwd)}, isHash)
	_, _ = svc.CreateUser(ctx, user.User{FullName: "grillo", Username: "grillo", Password: string(grilloPwd)}, isHash)

	// Create Keycloak OIDC provider for OAuth integration tests.
	// Only if KEYCLOAK_URL is set (e.g., http://localhost:8180).
	if kcURL := os.Getenv("KEYCLOAK_URL"); kcURL != "" {
		_, _ = svc.CreateOAuthProvider(ctx, oauthprovider.Provider{
			Name:          "Keycloak",
			Canonical:     "keycloak",
			Type:          "oidc",
			IssuerURL:     kcURL + "/realms/pikoci",
			ClientID:      "pikoci",
			ClientSecret:  "test-client-secret",
			Scopes:        "openid email profile",
			UsernameClaim: "preferred_username",
			Enabled:       true,
		})
	}

	go func() {
		runWorker(ctx, svc, 1, "DEBUG")
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
