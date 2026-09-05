package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/soheilhy/cmux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/config"
	"github.com/xescugc/duration"
	pikogrpc "github.com/pikoci/pikoci/pikoci/grpc"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/xyproto/randomstring"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/worker"
	"google.golang.org/grpc"

	"github.com/adrg/xdg"
)

// mainTeamCanonical is the default team canonical name used at startup.
var mainTeamCanonical = "main"

// serverViper is the viper instance used for server command flag and env var binding.
var serverViper = viper.New()

// serverCmd is the cobra command that starts the PikoCI server.
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
		wr := mysql.NewWorkerRepository(querier, cfg.DBSystem)
		atr := mysql.NewApiTokenRepository(querier)
		alr := mysql.NewAuditLogRepository(querier)
		opr := mysql.NewOAuthProviderRepository(querier)
		sr := mysql.NewSecretRepository(querier)

		suow := unitwork.NewStartUnitOfWork(db, cfg.DBSystem)

		wn := notifier.New()

		logger.Info("initializing service")
		var svc = pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, wr, atr, alr, opr, suow, jwtSecret, wn, logger)

		// The secret store is optional. With no --secret-key the server runs
		// exactly as before and secret operations report that they are not
		// configured, rather than failing startup for everyone who does not
		// use secrets.
		svc.EnableSecretStore(sr, cfg.SecretKey)
		if cfg.SecretKey == "" {
			logger.Info("secret encryption disabled: set --secret-key or PIKOCI_SECRET_KEY to store and read secrets. Plain config entries still work")
		}

		if cfg.SessionLifetime != "" && cfg.SessionLifetime != "0" {
			d, err := duration.Parse(cfg.SessionLifetime)
			if err != nil {
				return fmt.Errorf("invalid --session-lifetime value %q: %w", cfg.SessionLifetime, err)
			}
			svc.SessionLifetime = d
			logger.Info("session lifetime configured", "duration", d)
		}

		// Recover builds orphaned by a previous crash or unclean shutdown.
		if n, err := svc.RecoverOrphanedBuilds(ctx); err != nil {
			logger.Error("failed to recover orphaned builds", "error", err)
		} else if n > 0 {
			logger.Warn("failed orphaned builds from previous shutdown", "count", n)
		}

		svc.StartScheduler(ctx)
		logger.Info("initialized service")

		oauthStateStore := pikoci.NewOAuthStateStore(ctx)

		logger.Info("initializing http handlers")
		var handler = tshttp.Handler(svc, jwtSecret, logger.With("component", "HTTP"), db, cfg.DBSystem, Version, Commit, cfg.ExternalURL, oauthStateStore)
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

		workersGauge := prometheus.NewGaugeVec(
			prometheus.GaugeOpts{Name: "pikoci_workers", Help: "Number of registered workers by status."},
			[]string{"status"},
		)
		reg.MustRegister(workersGauge)

		// Update worker metrics periodically
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				workers, err := svc.ListWorkers(ctx)
				if err == nil {
					counts := map[string]float64{"healthy": 0, "stale": 0}
					for _, w := range workers {
						counts[string(w.Status)]++
					}
					for status, count := range counts {
						workersGauge.WithLabelValues(status).Set(count)
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()

		instrumentedHandler := promhttp.InstrumentHandlerCounter(httpRequests,
			promhttp.InstrumentHandlerDuration(httpDuration, handler),
		)

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		mux.Handle("/", instrumentedHandler)

		// Create gRPC server for worker streaming
		streamMgr := pikogrpc.NewWorkerStreamManager()
		grpcServer := pikogrpc.NewServer(svc, wn, streamMgr, jwtSecret, tr, logger.With("component", "gRPC"))
		grpcSrv := grpc.NewServer()
		workerv1.RegisterWorkerServiceServer(grpcSrv, grpcServer)

		// Store gRPC server on the service for cancellation routing
		svc.GRPCServer = grpcServer
		svc.TeamWorkerChecker = streamMgr

		svr := &http.Server{
			Handler: handlers.CombinedLoggingHandler(os.Stdout, mux),
		}

		errs := make(chan error, 1)

		// Use cmux to multiplex gRPC and HTTP on the same port
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err != nil {
			return fmt.Errorf("failed to listen on port %d: %w", cfg.Port, err)
		}
		m := cmux.New(lis)
		grpcLis := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
		httpLis := m.Match(cmux.Any())

		go func() {
			logger.Info("starting gRPC transport", "port", cfg.Port)
			if err := grpcSrv.Serve(grpcLis); err != nil {
				errs <- fmt.Errorf("gRPC server error: %w", err)
			}
		}()
		go func() {
			logger.Info("starting HTTP transport", "port", cfg.Port)
			if err := svr.Serve(httpLis); err != nil && err != http.ErrServerClosed {
				errs <- fmt.Errorf("HTTP server error: %w", err)
			}
		}()
		go func() {
			if err := m.Serve(); err != nil {
				// cmux.ErrListenerClosed is expected on shutdown
				select {
				case <-ctx.Done():
				default:
					errs <- fmt.Errorf("cmux error: %w", err)
				}
			}
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
			embeddedName := "embedded-" + randomstring.HumanFriendlyEnglishString(8)
			workers, wg, werr = runWorker(ctx, svc, cfg.Concurrency, cfg.LogLevel, embeddedName, nil, false)
			if werr != nil {
				return fmt.Errorf("worker failed to start: %w", werr)
			}
		}

		pipelineName := serverViper.GetString("pipeline-name")
		if pipelineName != "" {
			pipelineConfig := serverViper.GetString("pipeline-config")
			pipelineVars := serverViper.GetString("vars")
			teamCanonical := serverViper.GetString("team-canonical")
			err = createOrUpdatePipeline(ctx, svc, teamCanonical, pipelineName, pipelineConfig, pipelineVars)
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

			// Single deadline shared across both embedded and separated worker drain.
			deadline := time.After(drainTimeout)

			if cfg.RunWorker && workers != nil {
				for _, w := range workers {
					w.Drain()
				}
				logger.Info("workers draining, waiting for in-flight jobs to finish...", "timeout", drainTimeout)

				done := make(chan struct{})
				go func() { wg.Wait(); close(done) }()

				select {
				case <-done:
					logger.Info("all embedded workers finished")
				case <-deadline:
					logger.Warn("embedded worker drain timed out")
				}
			}

			// Wait for any remaining started builds (from separated workers).
			for {
				n, err := svc.CountStartedBuilds(ctx)
				if err != nil {
					logger.Error("failed to count started builds during drain", "error", err)
					break
				}
				if n == 0 {
					logger.Info("no running builds remaining")
					break
				}
				logger.Info("waiting for running builds to finish", "count", n)
				select {
				case <-time.After(2 * time.Second):
					continue
				case <-deadline:
					failed, err := svc.RecoverOrphanedBuilds(ctx)
					if err != nil {
						logger.Error("failed to recover orphaned builds during drain", "error", err)
					} else {
						logger.Warn("drain timeout expired, failed remaining builds", "count", failed)
					}
					goto shutdown
				}
			}
		shutdown:

			// Cancel the main context so any blocked Receive() calls
			// unblock and worker goroutines can exit.
			cancel()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()

			// GracefulStop doesn't accept a context, so enforce the
			// shutdown timeout manually and fall back to a hard Stop.
			grpcDone := make(chan struct{})
			go func() {
				grpcSrv.GracefulStop()
				close(grpcDone)
			}()
			select {
			case <-grpcDone:
			case <-shutdownCtx.Done():
				logger.Warn("gRPC graceful stop timed out, forcing stop")
				grpcSrv.Stop()
			}

			// shutdownCtx is shared: HTTP shutdown gets whatever time remains.
			svr.Shutdown(shutdownCtx)

		case sig := <-stop:
			logger.Info("received signal, shutting down immediately", "signal", sig)
			cancel()
			grpcSrv.Stop()
			svr.Close()

		case err := <-errs:
			logger.Error("component failed", "error", err)
			cancel()
			grpcSrv.Stop()
			svr.Close()
		}

		return nil
	},
}

func init() {
	serverCmd.Flags().StringP("config", "c", "", "Path to the config file")

	serverCmd.Flags().IntP("port", "p", 8080, "Port in which to start the server")
	serverCmd.Flags().String("jwt-secret", "", "Declares the Secret used to sign the JWT when user login")
	serverCmd.Flags().String("secret-key", "", "Master key used to encrypt stored secrets (env PIKOCI_SECRET_KEY). Optional: without it the secret store is unavailable. Losing it makes every stored secret unrecoverable")
	serverCmd.Flags().StringSlice("users", nil, "List of Users as 'USERNAME:HASH-PASSWORD' to create at startup or override default passwords. Use the 'user-password' command to generate hashes")
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
	serverCmd.Flags().String("log-level", "info", "Sets the log level ('debug', 'info', 'warn', 'error')")
	serverCmd.Flags().String("external-url", "", "External URL for OAuth callbacks (e.g. https://ci.pikoci.com)")
	serverCmd.Flags().String("session-lifetime", "0", "Maximum session duration before re-login is required (e.g. 24h, 7d, 30d). 0 means sessions never expire")
	serverCmd.Flags().String("team-canonical", mainTeamCanonical, "Team Canonical to scope the action")
	serverCmd.Flags().String("pipeline-config", "", "Path to the Pipeline config file")
	serverCmd.Flags().StringP("vars", "v", "", "Path to the Pipeline var file (JSON)")
	serverCmd.Flags().StringP("pipeline-name", "n", "", "Name of the Pipeline")

	// Bind all flags to viper
	serverViper.BindPFlags(serverCmd.Flags())

	// Env var support: JWT_SECRET, DB_SYSTEM, etc.
	serverViper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	serverViper.AutomaticEnv()

	// The master key is bound explicitly so it can be given the unambiguous
	// PIKOCI_SECRET_KEY name. AutomaticEnv alone would only match the bare
	// SECRET_KEY, which is generic enough to collide in a shared environment.
	serverViper.BindEnv("secret-key", "PIKOCI_SECRET_KEY", "SECRET_KEY")
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
