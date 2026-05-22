//go:build integration

package integration_test

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
	"github.com/pikoci/pikoci/pikoci/queue"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/mempubsub"
	"gocloud.dev/pubsub/natspubsub"
)

var pikoURL string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	jwtSecret := []byte("secret")
	ctx := context.Background()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})).With("service", "pikoci")

	db, err := mysql.New("", 0, "", "", mysql.Options{
		MultiStatements: true,
		ClientFoundRows: true,
		System:          mysql.Mem,
	})

	err = migrate.Migrate(db, mysql.Mem)
	if err != nil {
		panic(err)
	}

	topic, err := pubsub.OpenTopic(ctx, getTopicURL(mempubsub.Scheme))
	if err != nil {
		panic(fmt.Errorf("failed to open: %v", err).Error())
	}
	defer topic.Shutdown(ctx)

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
	var svc = pikoci.New(ctx, topic, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, suow, jwtSecret, logger)
	svc.StartScheduler(ctx)
	var handler = tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"))
	server := httptest.NewServer(handler)
	pikoURL = server.URL
	defer server.Close()

	isHash := true
	_, _ = svc.CreateUser(ctx, user.User{FullName: "pepito", Username: "pepito", Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm"}, isHash)
	_, _ = svc.CreateUser(ctx, user.User{FullName: "grillo", Username: "grillo", Password: "$2a$14$SvWir17.jlXxiZfe0pJuDedznetc/HWKv43YPsQQNo6MJiuypS2q6"}, isHash)
	go func() {
		runWorker(ctx, mempubsub.Scheme, topic, svc, 1, "DEBUG")
	}()

	return m.Run()
}

func getTopicURL(s string) string {
	u := fmt.Sprintf("%s://pikoci", s)
	switch s {
	case natspubsub.Scheme:
		u += "?natsv2"
	}
	return u
}

func getSubscriptionURL(s string) string {
	u := fmt.Sprintf("%s://pikoci", s)
	switch s {
	case natspubsub.Scheme:
		u += "?queue=pikoci&natsv2"
	}
	return u
}

func runWorker(ctx context.Context, sy string, t queue.Topic, s pikoci.Service, c int, llvl string) error {
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
	// Create a subscription connected to that topic.
	subscription, err := pubsub.OpenSubscription(ctx, getSubscriptionURL(sy))
	if err != nil {
		return fmt.Errorf("failed to OpenSubscription: %w", err)
	}
	defer subscription.Shutdown(ctx)

	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		nlogger := logger.With("num", i+1)
		nlogger.Info(fmt.Sprintf("Starting Worker %d", i+1))
		w := worker.New(s, t, subscription, nlogger)

		go func() {
			err = w.Run(ctx)
			if err != nil {
				logger.Error("failed to Run worker", "error", err)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
}
