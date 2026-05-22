package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/handlers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/config"
	"github.com/xescugc/pikoci/pikoci/mysql"
	"github.com/xescugc/pikoci/pikoci/mysql/migrate"
	tshttp "github.com/xescugc/pikoci/pikoci/transport/http"
	"github.com/xescugc/pikoci/pikoci/unitwork"
	"github.com/xescugc/pikoci/pikoci/user"
	"github.com/xescugc/pikoci/worker"
	"gocloud.dev/pubsub"

	"gocloud.dev/pubsub/mempubsub"
	"gocloud.dev/pubsub/natspubsub"

	_ "gocloud.dev/pubsub/kafkapubsub"
	_ "gocloud.dev/pubsub/rabbitpubsub"

	"github.com/adrg/xdg"
)

var mainTeamCanonical = "main"

var serverViper = viper.New()

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Starts the PikoCI server",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		// Load config file if provided
		cfgFile, _ := cmd.Flags().GetString("config")
		if cfgFile != "" {
			serverViper.SetConfigFile(cfgFile)
			if err := serverViper.ReadInConfig(); err != nil {
				return fmt.Errorf("error loading config file: %v", err)
			}
		}

		var cfg config.Config
		if err := serverViper.Unmarshal(&cfg); err != nil {
			return fmt.Errorf("error unmarshalling config: %v", err)
		}

		if cfg.JWTSecret == "" {
			return fmt.Errorf("required flag \"jwt-secret\" not set")
		}
		jwtSecret := []byte(cfg.JWTSecret)

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseSlogLevel(cfg.LogLevel)}))
		logger = logger.With("service", "pikoci")

		if cfg.DBSystem != mysql.Mem && cfg.DBSystem != mysql.MySQL && cfg.DBSystem != mysql.SQLite && cfg.DBSystem != mysql.PostgreSQL {
			return fmt.Errorf("invalid DBSystem %q, should be one of: %s, %s, %s or %s", cfg.DBSystem, mysql.Mem, mysql.MySQL, mysql.SQLite, mysql.PostgreSQL)
		}

		topic, err := pubsub.OpenTopic(ctx, getTopicURL(cfg.PubSubSystem))
		if err != nil {
			return fmt.Errorf("failed to open: %v", err)
		}
		defer topic.Shutdown(ctx)
		dbFile, err := xdg.DataFile(filepath.Join(AppName, AppName+".db"))
		if err != nil {
			return fmt.Errorf("failed to create dbFile: %v", err)
		}
		logger.Info("DB connection starting ...", "db-system", cfg.DBSystem)
		db, err := mysql.New(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, mysql.Options{
			DBName:          cfg.DBName,
			MultiStatements: true,
			ClientFoundRows: true,
			System:          cfg.DBSystem,
			DBFile:          dbFile,
		})
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		logger.Info("DB connection started", "db-system", cfg.DBSystem)

		if serverViper.GetBool("run-migrations") {
			logger.Info("Running migrations")
			err := migrate.Migrate(db, cfg.DBSystem)
			if err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}
			logger.Info("Migrations ran")
		}

		var querier sqlr.Querier = db
		if mysql.IsPostgreSQL(cfg.DBSystem) {
			querier = mysql.NewPGQuerier(db)
		}

		ur := mysql.NewUserRepository(querier)
		tr := mysql.NewTeamRepository(querier)
		ppr := mysql.NewPipelineRepository(querier)
		jr := mysql.NewJobRepository(querier)
		rr := mysql.NewResourceRepository(querier, cfg.DBSystem)
		rt := mysql.NewResourceTypeRepository(querier)
		br := mysql.NewBuildRepository(querier, cfg.DBSystem)
		rur := mysql.NewRunnerRepository(querier)
		str := mysql.NewSecretTypeRepository(querier)
		tgr := mysql.NewTriggerRepository(querier)

		suow := unitwork.NewStartUnitOfWork(db, cfg.DBSystem)

		logger.Info("initializing service")
		var svc = pikoci.New(ctx, topic, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, suow, jwtSecret, logger)
		svc.StartScheduler(ctx)
		logger.Info("initialized service")

		logger.Info("initializing http handlers")
		var handler = tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"))
		logger.Info("initialized http handlers")

		reg := prometheus.NewRegistry()
		reg.MustRegister(collectors.NewGoCollector())
		reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

		httpRequests := prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "http_requests_total", Help: "Total HTTP requests by status code and method."},
			[]string{"code", "method"},
		)
		httpDuration := prometheus.NewHistogramVec(
			prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "HTTP request duration in seconds."},
			[]string{"code", "method"},
		)
		reg.MustRegister(httpRequests, httpDuration)

		instrumentedHandler := promhttp.InstrumentHandlerCounter(httpRequests,
			promhttp.InstrumentHandlerDuration(httpDuration, handler),
		)

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		mux.Handle("/", instrumentedHandler)

		svr := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Port),
			Handler: handlers.CombinedLoggingHandler(os.Stdout, mux),
		}

		errs := make(chan error, 1)

		go func() {
			logger.Info("starting HTTP transport", "port", cfg.Port)
			errs <- svr.ListenAndServe()
		}()

		if !cfg.RunWorker {
			wt := generateWorkerJWT(jwtSecret)
			logger.Info("Worker token for standalone workers", "token", wt)
		}

		var workers []*worker.Worker
		var wg *sync.WaitGroup
		if cfg.RunWorker {
			logger.Info("Starting Worker ...")
			var werr error
			var workerCleanup func()
			workers, wg, workerCleanup, werr = runWorker(ctx, cfg.PubSubSystem, topic, svc, cfg.Concurrency, cfg.LogLevel)
			if werr != nil {
				return fmt.Errorf("worker failed to start: %w", werr)
			}
			defer workerCleanup()
		}

		pipelineName := serverViper.GetString("pipeline-name")
		if pipelineName != "" {
			pipelineConfig := serverViper.GetString("pipeline-config")
			pipelineVars := serverViper.GetString("pipeline-vars")
			teamCanonical := serverViper.GetString("team-canonical")
			err = createPipeline(ctx, svc, teamCanonical, pipelineName, pipelineConfig, pipelineVars)
			if err != nil {
				return err
			}
		}
		if users := cfg.Users; len(users) != 0 {
			for _, u := range users {
				us := strings.SplitN(u, userPasswordSeparator, 2)
				if len(us) != 2 {
					return fmt.Errorf("invalid user format %q, expected USERNAME:HASH", u)
				}
				isHashed := true
				_, err = svc.CreateOrUpdateUser(ctx, user.User{FullName: us[0], Username: us[0], Password: us[1]}, isHashed)
				if err != nil {
					return fmt.Errorf("failed to create user %q: %w", us[0], err)
				}
			}
		}

		drainTimeout, err := time.ParseDuration(cfg.DrainTimeout)
		if err != nil {
			return fmt.Errorf("invalid drain-timeout %q: %w", cfg.DrainTimeout, err)
		}

		quit := make(chan os.Signal, 1)
		stop := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGQUIT)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

		select {
		case sig := <-quit:
			logger.Info("received signal, starting graceful shutdown", "signal", sig)

			if cfg.RunWorker && workers != nil {
				for _, w := range workers {
					w.Drain()
				}
				logger.Info("workers draining, waiting for in-flight jobs to finish...", "timeout", drainTimeout)

				done := make(chan struct{})
				go func() { wg.Wait(); close(done) }()

				select {
				case <-done:
					logger.Info("all workers finished")
				case <-time.After(drainTimeout):
					logger.Warn("graceful shutdown timed out, forcing exit")
				}
			}

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()
			svr.Shutdown(shutdownCtx)

		case sig := <-stop:
			logger.Info("received signal, shutting down immediately", "signal", sig)
			cancel()
			svr.Close()

		case err := <-errs:
			logger.Error("component failed", "error", err)
			cancel()
			svr.Close()
		}

		return nil
	},
}

func init() {
	serverCmd.Flags().StringP("config", "c", "", "Path to the config file")

	serverCmd.Flags().IntP("port", "p", 8080, "Port in which to start the server")
	serverCmd.Flags().String("jwt-secret", "", "Declares the Secret used to sign the JWT when user login")
	serverCmd.Flags().StringSlice("users", nil, "List of Users which will have 'USERNAME:HASH-PASSWORD', you can use the 'user-password' command to help you")
	serverCmd.Flags().String("db-system", mysql.Mem, "Which DB system to use (mem, sqlite, mysql, postgresql)")
	serverCmd.Flags().String("db-host", "", "Database Host")
	serverCmd.Flags().Int("db-port", 0, "Database Port")
	serverCmd.Flags().String("db-user", "", "Database User")
	serverCmd.Flags().String("db-password", "", "Database Password")
	serverCmd.Flags().String("db-name", "", "Database Name")
	serverCmd.Flags().Bool("run-migrations", true, "Flag to know if migrations should be ran")
	serverCmd.Flags().Bool("run-worker", true, "Runs a worker with PikoCI server")
	serverCmd.Flags().Int("concurrency", 1, "Number of workers to start in one instance")
	serverCmd.Flags().String("drain-timeout", "10m", "Maximum time to wait for in-flight jobs to finish during graceful shutdown (SIGQUIT)")
	serverCmd.Flags().String("pubsub-system", mempubsub.Scheme, "Which PubSub system to use (mem, nats, rabbit, kafka). Env vars: NATS_SERVER_URL, RABBIT_SERVER_URL, KAFKA_BROKERS")
	serverCmd.Flags().String("log-level", "info", "Sets the log level ('debug', 'info', 'warn', 'error')")
	serverCmd.Flags().String("team-canonical", mainTeamCanonical, "Team Canonical to scope the action")
	serverCmd.Flags().String("pipeline-config", "", "Path to the Pipeline config file")
	serverCmd.Flags().StringP("pipeline-vars", "v", "", "Path to the Pipeline var file (JSON)")
	serverCmd.Flags().StringP("pipeline-name", "n", "", "Name of the Pipeline")

	// Bind all flags to viper
	serverViper.BindPFlags(serverCmd.Flags())

	// Env var support: JWT_SECRET, DB_SYSTEM, etc.
	serverViper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	serverViper.AutomaticEnv()
}

func getSubscriptionURL(s string) string {
	u := fmt.Sprintf("%s://pikoci", s)
	switch s {
	case natspubsub.Scheme:
		u += "?queue=pikoci&natsv2"
	case "rabbit":
		// rabbit://pikoci — uses RABBIT_SERVER_URL env var
	case "kafka":
		u = "kafka://pikoci-group?topic=pikoci"
	}
	return u
}

func getTopicURL(s string) string {
	u := fmt.Sprintf("%s://pikoci", s)
	switch s {
	case natspubsub.Scheme:
		u += "?natsv2"
	case "rabbit":
		// rabbit://pikoci — uses RABBIT_SERVER_URL env var
	case "kafka":
		// kafka://pikoci — uses KAFKA_BROKERS env var
	}
	return u
}

func parseSlogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func generateWorkerJWT(js []byte) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	tokenString, err := token.SignedString(js)
	if err != nil {
		panic(err)
	}
	return tokenString
}
