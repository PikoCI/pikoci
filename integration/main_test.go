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

	jobTopic, err := pubsub.OpenTopic(ctx, getTopicURL(mempubsub.Scheme, "pikoci-jobs"))
	if err != nil {
		panic(fmt.Errorf("failed to open job topic: %v", err).Error())
	}
	defer jobTopic.Shutdown(ctx)

	checkTopic, err := pubsub.OpenTopic(ctx, getTopicURL(mempubsub.Scheme, "pikoci-checks"))
	if err != nil {
		panic(fmt.Errorf("failed to open check topic: %v", err).Error())
	}
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
	var svc = pikoci.New(ctx, jobTopic, checkTopic, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, suow, jwtSecret, logger)
	svc.StartScheduler(ctx)
	var handler = tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, mysql.Mem, "test", "abc1234")
	server := httptest.NewServer(handler)
	pikoURL = server.URL
	defer server.Close()

	isHash := true
	_, _ = svc.CreateUser(ctx, user.User{FullName: "pepito", Username: "pepito", Password: "$2a$14$rwQk8Qvc2rij7qhFO4P1W.OiSF6AkgVU1RCrLaY2wawJcpkPEKwbm"}, isHash)
	_, _ = svc.CreateUser(ctx, user.User{FullName: "grillo", Username: "grillo", Password: "$2a$14$SvWir17.jlXxiZfe0pJuDedznetc/HWKv43YPsQQNo6MJiuypS2q6"}, isHash)
	go func() {
		runWorker(ctx, mempubsub.Scheme, jobTopic, svc, 1, "DEBUG")
	}()

	return m.Run()
}

func getTopicURL(s, name string) string {
	u := fmt.Sprintf("%s://%s", s, name)
	switch s {
	case natspubsub.Scheme:
		u += "?natsv2"
	}
	return u
}

func getSubscriptionURL(s, name string) string {
	u := fmt.Sprintf("%s://%s", s, name)
	switch s {
	case natspubsub.Scheme:
		u += fmt.Sprintf("?queue=%s&natsv2", name)
	}
	return u
}

func runWorker(ctx context.Context, sy string, jobTopic queue.Topic, s pikoci.Service, c int, llvl string) error {
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

	jobSub, err := pubsub.OpenSubscription(ctx, getSubscriptionURL(sy, "pikoci-jobs"))
	if err != nil {
		return fmt.Errorf("failed to open job subscription: %w", err)
	}
	defer jobSub.Shutdown(ctx)

	checkSub, err := pubsub.OpenSubscription(ctx, getSubscriptionURL(sy, "pikoci-checks"))
	if err != nil {
		return fmt.Errorf("failed to open check subscription: %w", err)
	}
	defer checkSub.Shutdown(ctx)

	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		nlogger := logger.With("num", i+1)
		nlogger.Info(fmt.Sprintf("Starting Worker %d", i+1))
		w := worker.New(s, jobTopic, jobSub, checkSub, nlogger)

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
