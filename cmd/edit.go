package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/cycloidio/sqlr"
	"github.com/lopezator/migrator"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	tshttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Pipeline management commands",
}

var editCmd = &cobra.Command{
	Use:   "edit <file.hcl>",
	Short: "Open the pipeline editor in a browser for a local HCL file",
	Long:  "Start a local HTTP server and open the browser-based pipeline editor pre-loaded with the given HCL file. Changes saved in the editor are written back to disk.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
		port, _ := cmd.Flags().GetInt("port")

		// Verify the file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", filePath)
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

		// Create in-memory DB (same pattern as cmd/run.go)
		db, err := mysql.New("", 0, "", "", mysql.Options{
			System:          mysql.Mem,
			MultiStatements: true,
			ClientFoundRows: true,
		})
		if err != nil {
			return fmt.Errorf("failed to create in-memory database: %w", err)
		}
		defer db.Close()

		nopLogger := migrator.WithLogger(migrator.LoggerFunc(func(string, ...interface{}) {}))
		if err := migrate.Migrate(db, mysql.Mem, nopLogger); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		var querier sqlr.Querier = db
		ur := mysql.NewUserRepository(querier)
		tr := mysql.NewTeamRepository(querier)
		ppr := mysql.NewPipelineRepository(querier)
		jr := mysql.NewJobRepository(querier)
		rr := mysql.NewResourceRepository(querier, mysql.Mem)
		rt := mysql.NewResourceTypeRepository(querier)
		br := mysql.NewBuildRepository(querier, mysql.Mem)
		rur := mysql.NewRunnerRepository(querier)
		str := mysql.NewSecretTypeRepository(querier)
		tgr := mysql.NewTriggerRepository(querier)
		suow := unitwork.NewStartUnitOfWork(db, mysql.Mem)

		ctx := cmd.Context()
		jwtSecret := []byte("local-edit-secret")
		svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, nil, nil, nil, suow, jwtSecret, nil, logger)

		handler := tshttp.LocalEditorHandler(svc, filePath, logger)

		listenAddr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := net.Listen("tcp", listenAddr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
		}

		actualAddr := listener.Addr().String()
		url := "http://" + actualAddr
		fmt.Fprintf(os.Stderr, "Local editor running at %s\n", url)
		fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop\n")

		// Open browser
		go openBrowser(url)

		server := &http.Server{Handler: handler}

		// Graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Fprintf(os.Stderr, "\nShutting down...\n")
			server.Shutdown(context.Background())
		}()

		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	},
}

func init() {
	editCmd.Flags().Int("port", 0, "Port to listen on (0 for random available port)")
	pipelineCmd.AddCommand(editCmd)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		fmt.Fprintf(os.Stderr, "Open %s in your browser\n", url)
		return
	}
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser: %v\nOpen %s manually\n", err, url)
	}
}
