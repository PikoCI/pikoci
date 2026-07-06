package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/cycloidio/sqlr"
	"github.com/spf13/cobra"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/mysql"
	"github.com/lopezator/migrator"
	"github.com/pikoci/pikoci/pikoci/mysql/migrate"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/unitwork"
	"github.com/pikoci/pikoci/worker"
)

// ExitError wraps an exit code so cobra's RunE can propagate it without
// calling os.Exit directly, which would bypass deferred cleanup.
type ExitError struct {
	// Code is the process exit code to return.
	Code int
}

// Error returns a string representation of the exit code.
func (e *ExitError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

var runCmd = &cobra.Command{
	Use:           "run",
	Short:         "Run a pipeline job locally",
	Long:          "Execute a single job from a pipeline HCL file directly on the local machine without needing a server.",
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pipelineConfig, _ := cmd.Flags().GetString("pipeline-config")
		jobName, _ := cmd.Flags().GetString("job")
		varFlags, _ := cmd.Flags().GetStringSlice("var")
		varsFile, _ := cmd.Flags().GetString("vars")
		resourceFlags, _ := cmd.Flags().GetStringSlice("resource")
		logLevel, _ := cmd.Flags().GetString("log-level")

		if pipelineConfig == "" {
			return fmt.Errorf("required flag \"pipeline-config\" not set")
		}
		if jobName == "" {
			return fmt.Errorf("required flag \"job\" not set")
		}

		vars, err := buildVars(varFlags, varsFile)
		if err != nil {
			return err
		}

		resourceOverrides, err := parseResourceFlags(resourceFlags)
		if err != nil {
			return err
		}

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseSlogLevel(logLevel)}))

		exitCode, err := runLocal(cmd.Context(), logger, pipelineConfig, jobName, vars, resourceOverrides)
		if err != nil {
			return err
		}
		if exitCode != 0 {
			return &ExitError{Code: exitCode}
		}
		return nil
	},
}

func init() {
	runCmd.Flags().StringP("pipeline-config", "p", "", "Path to the pipeline HCL file")
	runCmd.Flags().StringP("job", "j", "", "Job name to execute")
	runCmd.Flags().StringSlice("var", nil, "Variable overrides in key=value format (repeatable)")
	runCmd.Flags().StringP("vars", "v", "", "Path to the Pipeline var file (JSON)")
	runCmd.Flags().StringSlice("resource", nil, "Resource overrides in type.name=path format (repeatable, e.g. git.my-repo=./local-dir)")
	runCmd.Flags().String("log-level", "error", "Sets the log level ('debug', 'info', 'warn', 'error')")
}

func buildVars(varFlags []string, varsFile string) (map[string]interface{}, error) {
	vars := make(map[string]interface{})

	if varsFile != "" {
		f, err := os.Open(varsFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open vars file %q: %w", varsFile, err)
		}
		defer f.Close()
		if err := json.NewDecoder(f).Decode(&vars); err != nil {
			return nil, fmt.Errorf("failed to decode vars file %q: %w", varsFile, err)
		}
	}

	for _, v := range varFlags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --var format %q, expected key=value", v)
		}
		vars[parts[0]] = parts[1]
	}

	if len(vars) == 0 {
		return nil, nil
	}
	return vars, nil
}

func parseResourceFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	overrides := make(map[string]string)
	for _, f := range flags {
		parts := strings.SplitN(f, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --resource format %q, expected name=path", f)
		}
		overrides[parts[0]] = parts[1]
	}
	return overrides, nil
}

func runLocal(ctx context.Context, logger *slog.Logger, pipelineConfig, jobName string, vars map[string]interface{}, resourceOverrides map[string]string) (int, error) {
	// Read the pipeline HCL file
	hclBytes, err := os.ReadFile(pipelineConfig)
	if err != nil {
		return 0, fmt.Errorf("failed to read pipeline config %q: %w", pipelineConfig, err)
	}

	// Create in-memory DB
	db, err := mysql.New("", 0, "", "", mysql.Options{
		System:          mysql.Mem,
		MultiStatements: true,
		ClientFoundRows: true,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create in-memory database: %w", err)
	}
	defer db.Close()

	// Run migrations silently (creates "main" team + "admin" user)
	nopLogger := migrator.WithLogger(migrator.LoggerFunc(func(string, ...interface{}) {}))
	if err := migrate.Migrate(db, mysql.Mem, nopLogger); err != nil {
		return 0, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create repositories
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

	// Create service (do NOT start scheduler)
	jwtSecret := []byte("local-run-secret")
	svc := pikoci.New(ctx, ur, tr, ppr, jr, rr, rt, br, rur, str, tgr, nil, nil, nil, suow, jwtSecret, notifier.New(), logger)

	// Create pipeline
	pp, err := svc.CreatePipeline(ctx, mainTeamCanonical, "local", hclBytes, vars)
	if err != nil {
		return 0, fmt.Errorf("failed to create pipeline: %w", err)
	}

	// Verify the job exists
	_, err = svc.GetPipelineJob(ctx, mainTeamCanonical, "local", jobName)
	if err != nil {
		return 0, fmt.Errorf("job %q not found in pipeline: %w", jobName, err)
	}

	// Seed synthetic resource versions for each resource so get steps have something to pull
	for _, r := range pp.Resources {
		_, err := svc.CreateResourceVersion(ctx, mainTeamCanonical, "local", r.Canonical, resource.Version{
			Version: map[string]interface{}{"ref": "HEAD"},
		})
		if err != nil {
			return 0, fmt.Errorf("failed to seed resource version for %q: %w", r.Canonical, err)
		}
	}

	// Build resource override map: keys must use canonical format (type.name)
	var workerResourceOverrides map[string]string
	if len(resourceOverrides) > 0 {
		workerResourceOverrides = make(map[string]string)
		for canonical, path := range resourceOverrides {
			found := false
			for _, r := range pp.Resources {
				if r.Canonical == canonical {
					found = true
					break
				}
			}
			if !found {
				return 0, fmt.Errorf("resource %q not found in pipeline (use type.name format, e.g. git.my-repo)", canonical)
			}
			workerResourceOverrides[canonical] = path
		}
	}

	// Create worker with overrides
	workerLogger := logger.With("service", "worker")
	w := worker.New(svc, workerLogger, "local-run", "", "", 1, nil, false)
	w.ResourceOverrides = workerResourceOverrides
	w.LocalMode = true

	// Start worker in goroutine
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	workerDone := make(chan error, 1)
	go func() {
		workerDone <- w.Run(workerCtx)
	}()

	// Trigger the job
	if err := svc.TriggerPipelineJob(ctx, mainTeamCanonical, "local", jobName); err != nil {
		return 0, fmt.Errorf("failed to trigger job %q: %w", jobName, err)
	}

	// Poll loop: stream output and wait for terminal status
	printedSteps := 0
	printedBytes := make(map[int]int)
	printedSubSteps := make(map[int]int)   // per parent step: how many sub-steps printed
	printedSubBytes := make(map[string]int) // "parentIdx-subIdx" -> bytes printed
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 1, ctx.Err()
		case err := <-workerDone:
			if err != nil {
				return 1, fmt.Errorf("worker error: %w", err)
			}
			// Worker exited cleanly but shouldn't before we're done polling
			// This can happen if context is cancelled
			return 1, nil
		case <-ticker.C:
			builds, _, err := svc.ListJobBuilds(ctx, mainTeamCanonical, "local", jobName, nil, nil, 0, nil)
			if err != nil {
				continue
			}
			if len(builds) == 0 {
				continue
			}

			b := builds[0] // most recent build

			// Print incremental log updates for already-seen steps first,
			// so existing step output completes before new step headers.
			for i := 0; i < printedSteps && i < len(b.Steps); i++ {
				step := b.Steps[i]
				prev := printedBytes[i]
				if len(step.Logs) > prev {
					delta := step.Logs[prev:]
					fmt.Fprint(os.Stdout, delta)
					if !strings.HasSuffix(delta, "\n") {
						fmt.Fprintln(os.Stdout)
					}
					printedBytes[i] = len(step.Logs)
				}
				// Incrementally print new/updated sub-steps for in_parallel
				if len(step.SubSteps) > 0 {
					prevSubs := printedSubSteps[i]
					for si := prevSubs; si < len(step.SubSteps); si++ {
						sub := step.SubSteps[si]
						fmt.Fprintf(os.Stdout, "  ==> [%s] %s\n", sub.Type, sub.Name)
						if len(sub.Logs) > 0 {
							fmt.Fprint(os.Stdout, sub.Logs)
							if !strings.HasSuffix(sub.Logs, "\n") {
								fmt.Fprintln(os.Stdout)
							}
							printedSubBytes[fmt.Sprintf("%d-%d", i, si)] = len(sub.Logs)
						}
					}
					// Update logs for already-printed sub-steps
					for si := 0; si < prevSubs && si < len(step.SubSteps); si++ {
						sub := step.SubSteps[si]
						key := fmt.Sprintf("%d-%d", i, si)
						prevBytes := printedSubBytes[key]
						if len(sub.Logs) > prevBytes {
							delta := sub.Logs[prevBytes:]
							fmt.Fprint(os.Stdout, delta)
							if !strings.HasSuffix(delta, "\n") {
								fmt.Fprintln(os.Stdout)
							}
							printedSubBytes[key] = len(sub.Logs)
						}
					}
					printedSubSteps[i] = len(step.SubSteps)
				}
			}

			// Print new steps and their logs
			for i := printedSteps; i < len(b.Steps); i++ {
				step := b.Steps[i]
				if step.Type == "in_parallel" {
					fmt.Fprintf(os.Stdout, "==> [%s] (%d steps)\n", step.Type, len(step.SubSteps))
				} else {
					fmt.Fprintf(os.Stdout, "==> [%s] %s\n", step.Type, step.Name)
				}
				if len(step.Logs) > 0 {
					fmt.Fprint(os.Stdout, step.Logs)
					if !strings.HasSuffix(step.Logs, "\n") {
						fmt.Fprintln(os.Stdout)
					}
					printedBytes[i] = len(step.Logs)
				}
				// Print any sub-steps already present
				for si, sub := range step.SubSteps {
					fmt.Fprintf(os.Stdout, "  ==> [%s] %s\n", sub.Type, sub.Name)
					if len(sub.Logs) > 0 {
						fmt.Fprint(os.Stdout, sub.Logs)
						if !strings.HasSuffix(sub.Logs, "\n") {
							fmt.Fprintln(os.Stdout)
						}
						printedSubBytes[fmt.Sprintf("%d-%d", i, si)] = len(sub.Logs)
					}
				}
				printedSubSteps[i] = len(step.SubSteps)
				printedSteps = i + 1
			}

			// Check if build is terminal
			if b.Status == build.Succeeded || b.Status == build.Failed || b.Status == build.Cancelled {
				// Print job-level logs (hooks like on_success, on_failure, ensure)
				for _, step := range b.Job {
					fmt.Fprintf(os.Stdout, "==> [%s] %s\n", step.Type, step.Name)
					if len(step.Logs) > 0 {
						fmt.Fprint(os.Stdout, step.Logs)
						if !strings.HasSuffix(step.Logs, "\n") {
							fmt.Fprintln(os.Stdout)
						}
					}
				}

				workerCancel()
				switch b.Status {
				case build.Succeeded:
					fmt.Fprintf(os.Stdout, "\n✓ job %q succeeded\n", jobName)
					return 0, nil
				case build.Failed:
					fmt.Fprintf(os.Stdout, "\n✗ job %q failed\n", jobName)
					return 1, nil
				case build.Cancelled:
					fmt.Fprintf(os.Stdout, "\n⊘ job %q cancelled\n", jobName)
					return 1, nil
				}
			}
		}
	}
}
