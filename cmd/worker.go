package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/transport/http/client"
	"github.com/pikoci/pikoci/pikoci/wkr"
	"github.com/pikoci/pikoci/worker"
	"github.com/pikoci/pikoci/worker/config"
	"github.com/xyproto/randomstring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// workerViper is the viper instance used for worker command flag and env var binding.
var workerViper = viper.New()

// workerCmd is the cobra command that starts a standalone PikoCI worker.
var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Starts a PikoCI Worker",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		// Load config file if provided
		cfgFile, _ := cmd.Flags().GetString("config")
		if cfgFile != "" {
			workerViper.SetConfigFile(cfgFile)
			if err := workerViper.ReadInConfig(); err != nil {
				return fmt.Errorf("error loading config file: %v", err)
			}
		}

		var cfg config.Config
		if err := workerViper.Unmarshal(&cfg); err != nil {
			return fmt.Errorf("error unmarshalling config: %v", err)
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseSlogLevel(cfg.LogLevel)}))

		if cfg.WorkerToken == "" {
			return fmt.Errorf("required flag \"worker-token\" not set")
		}
		workerToken := cfg.WorkerToken
		// HTTP client is still used for data queries (GetPipeline, UpdateJobBuild, etc.)
		c, err := client.New(cfg.PikoCIURL, workerToken)
		if err != nil {
			return fmt.Errorf("failed to initialize client with url %q: %w", cfg.PikoCIURL, err)
		}

		// Resolve gRPC target from the PikoCI URL
		grpcTarget := resolveGRPCTarget(cfg.PikoCIURL)
		logger.Info("connecting to gRPC server", "target", grpcTarget)

		grpcConn, err := grpc.NewClient(grpcTarget,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return fmt.Errorf("failed to create gRPC connection: %w", err)
		}
		defer grpcConn.Close()
		grpcClient := workerv1.NewWorkerServiceClient(grpcConn)

		drainTimeout, err := time.ParseDuration(cfg.DrainTimeout)
		if err != nil {
			return fmt.Errorf("invalid drain-timeout %q: %w", cfg.DrainTimeout, err)
		}

		name := cfg.Name
		if name == "" {
			name = randomstring.HumanFriendlyEnglishString(16)
		}

		var tags []string
		if cfg.Tags != "" {
			for _, t := range strings.Split(cfg.Tags, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}
		if err := wkr.ValidateTags(tags); err != nil {
			return fmt.Errorf("invalid worker tags: %w", err)
		}
		if cfg.ExclusiveTags && len(tags) == 0 {
			return fmt.Errorf("--exclusive-tags requires at least one --tags value")
		}

		workers, wg, err := runGRPCWorker(ctx, c, grpcClient, workerToken, grpcTarget, cfg.Concurrency, cfg.LogLevel, name, tags, cfg.ExclusiveTags)
		if err != nil {
			return fmt.Errorf("failed to start worker: %w", err)
		}

		quit := make(chan os.Signal, 1)
		stop := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGQUIT)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

		errs := make(chan error, 1)
		go func() {
			wg.Wait()
			errs <- nil
		}()

		select {
		case sig := <-quit:
			logger.Info("received signal, starting graceful shutdown", "signal", sig)
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

		case sig := <-stop:
			logger.Info("received signal, shutting down immediately", "signal", sig)
			cancel()

		case err := <-errs:
			if err != nil {
				logger.Error("worker failed", "error", err)
				return err
			}
		}

		return nil
	},
}

func init() {
	workerCmd.Flags().StringP("config", "c", "", "Path to the config file")
	workerCmd.Flags().StringP("pikoci-url", "u", "localhost:8080", "URL to the PikoCI server")
	workerCmd.Flags().String("name", "", "Worker name (auto-generated if empty)")
	workerCmd.Flags().String("tags", "", "Comma-separated worker tags for job/resource routing (e.g. gpu,vpn)")
	workerCmd.Flags().Bool("exclusive-tags", false, "When set, worker only handles work matching its tags (skips untagged jobs/resources)")
	workerCmd.Flags().Int("concurrency", 1, "Number of workers to start in one instance")
	workerCmd.Flags().String("drain-timeout", "10m", "Maximum time to wait for in-flight jobs to finish during graceful shutdown (SIGQUIT)")
	workerCmd.Flags().String("log-level", "info", "Sets the log level ('debug', 'info', 'warn', 'error')")
	workerCmd.Flags().String("worker-token", "", "Worker authentication token (from 'pikoci worker-token' or server startup logs)")

	workerViper.BindPFlags(workerCmd.Flags())

	workerViper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	workerViper.AutomaticEnv()
}

func runWorker(ctx context.Context, s pikoci.Service, c int, llvl string, name string, tags []string, exclusiveTags bool) ([]*worker.Worker, *sync.WaitGroup, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseSlogLevel(llvl)}))
	logger = logger.With("service", "worker")

	var workers []*worker.Worker
	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		workerName := name
		if c > 1 {
			workerName = fmt.Sprintf("%s-%d", name, i+1)
		}
		nlogger := logger.With("num", i+1, "name", workerName)
		nlogger.Info(fmt.Sprintf("Starting Worker %d", i+1))
		w := worker.New(s, nlogger, workerName, Version, c, tags, exclusiveTags)
		workers = append(workers, w)

		go func() {
			defer wg.Done()
			err := w.Run(ctx)
			if err != nil {
				logger.Error("failed to Run worker", "error", err)
			}
		}()
	}
	return workers, &wg, nil
}

// runGRPCWorker creates workers configured for gRPC streaming.
func runGRPCWorker(ctx context.Context, s pikoci.Service, grpcClient workerv1.WorkerServiceClient, workerToken, grpcAddr string, c int, llvl string, name string, tags []string, exclusiveTags bool) ([]*worker.Worker, *sync.WaitGroup, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseSlogLevel(llvl)}))
	logger = logger.With("service", "worker")

	var workers []*worker.Worker
	var wg sync.WaitGroup
	for i := range c {
		wg.Add(1)
		workerName := name
		if c > 1 {
			workerName = fmt.Sprintf("%s-%d", name, i+1)
		}
		nlogger := logger.With("num", i+1, "name", workerName)
		nlogger.Info(fmt.Sprintf("Starting gRPC Worker %d", i+1))
		w := worker.NewGRPC(s, grpcClient, nlogger, workerName, Version, c, workerToken, grpcAddr, tags, exclusiveTags)
		workers = append(workers, w)

		go func() {
			defer wg.Done()
			err := w.Run(ctx)
			if err != nil {
				logger.Error("failed to Run worker", "error", err)
			}
		}()
	}
	return workers, &wg, nil
}

// resolveGRPCTarget extracts the host:port from a URL for use as a gRPC target.
func resolveGRPCTarget(pikociURL string) string {
	target := pikociURL
	target = strings.TrimPrefix(target, "http://")
	target = strings.TrimPrefix(target, "https://")
	// Remove any trailing path
	if idx := strings.Index(target, "/"); idx != -1 {
		target = target[:idx]
	}
	return target
}
