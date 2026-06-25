// Package worker implements the background job execution engine for PikoCI.
// It polls for work items (job builds and resource checks) via the server API
// and executes the corresponding pipeline steps using the configured runners
// and resource types.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adrg/xdg"
	"github.com/agnivade/levenshtein"
	"github.com/google/uuid"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/service"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/pikoci/pikoci/pikoci/wkr"
	"gopkg.in/yaml.v3"
)

// Service is the interface for running the worker event loop. Implementations
// poll for work items and process them until the context is cancelled.
type Service interface {
	// Run starts the worker loop, polling for work items and processing them.
	Run(ctx context.Context, q, t string) error
}

// WorkPoller is implemented by *pikoci.PikoCI and provides PollNextWork
// for the embedded worker's poll loop. It is not part of the Service interface
// because standalone workers use gRPC streaming instead.
type WorkPoller interface {
	PollNextWork(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error)
}

// Worker processes job builds and resource checks received via HTTP long
// polling or gRPC streaming. It manages build lifecycle, executes pipeline
// steps, and supports graceful draining.
type Worker struct {
	pikoci            pikoci.Service
	workPoller        WorkPoller

	draining      atomic.Bool
	drainCancelMu sync.Mutex
	drainCancel   context.CancelFunc
	logger      *slog.Logger

	// apiCtx is the parent server context used for DB operations.
	// It outlives individual job contexts (which get cancelled on
	// build cancellation) but is cancelled on server shutdown.
	apiCtx context.Context

	// ResourceOverrides maps resource canonical names to local directory paths.
	// When set, get steps for these resources will copy the local directory
	// instead of running the resource type's pull command.
	ResourceOverrides map[string]string
	// LocalMode enables local execution behavior: skips passed-constraints,
	// version-availability checks, put steps, and secret resolution.
	LocalMode bool

	// Heartbeat info
	Name          string
	Tags          []string
	ExclusiveTags bool
	StartedAt     time.Time
	Concurrency   int
	Version       string
	Commit        string

	// GRPCAddr is the address of the gRPC server. When set, the worker uses
	// gRPC streaming instead of HTTP long polling. When empty (embedded worker),
	// it uses pollLoop.
	GRPCAddr string
	// WorkerToken is the JWT token used for gRPC registration.
	WorkerToken string

	// grpcClient is the gRPC client for the WorkerService.
	grpcClient workerv1.WorkerServiceClient
	// grpcStream is the active Execute stream (nil when not connected).
	// Protected by grpcStreamMu.
	grpcStreamMu sync.Mutex
	grpcStream   workerv1.WorkerService_ExecuteClient

	// jobCancels tracks active job cancel functions for gRPC cancellation.
	jobCancelsMu sync.Mutex
	jobCancels   map[string]context.CancelFunc
}

func (w *Worker) cacheDir(teamCanonical, pipelineCanonical, resourceCanonical string) (string, error) {
	p, err := xdg.CacheFile(filepath.Join("pikoci", "cache", teamCanonical, pipelineCanonical, resourceCanonical, ".keep"))
	if err != nil {
		return "", fmt.Errorf("failed to create cache dir: %w", err)
	}
	return filepath.Dir(p), nil
}

func resourceCacheEnabled(rt restype.ResourceType, r resource.Resource) bool {
	if r.Cache != nil {
		return *r.Cache
	}
	return rt.Cache
}

// New creates a new Worker with the given PikoCI service and logger. The
// returned Worker is ready to be started with Run, which uses HTTP long-poll
// to receive work items (embedded mode). The service must implement WorkPoller
// (satisfied by *pikoci.PikoCI) for the embedded poll loop.
func New(s pikoci.Service, l *slog.Logger, name, version, commit string, concurrency int, tags []string, exclusiveTags bool) *Worker {
	w := &Worker{
		pikoci:        s,
		logger:        l,
		Name:          name,
		Tags:          tags,
		ExclusiveTags: exclusiveTags,
		StartedAt:     time.Now(),
		Concurrency:   concurrency,
		Version:       version,
		Commit:        commit,
	}
	if wp, ok := s.(WorkPoller); ok {
		w.workPoller = wp
	}
	return w
}

// NewGRPC creates a Worker configured for gRPC streaming mode.
func NewGRPC(s pikoci.Service, gc workerv1.WorkerServiceClient, l *slog.Logger, name, version, commit string, concurrency int, workerToken, grpcAddr string, tags []string, exclusiveTags bool) *Worker {
	return &Worker{
		pikoci:        s,
		grpcClient:    gc,
		logger:        l,
		Name:          name,
		Tags:          tags,
		ExclusiveTags: exclusiveTags,
		StartedAt:     time.Now(),
		Concurrency:   concurrency,
		Version:       version,
		Commit:        commit,
		GRPCAddr:      grpcAddr,
		WorkerToken:   workerToken,
	}
}

// Drain signals the worker to stop accepting new messages. In-flight jobs
// continue to completion, but the receive loops are cancelled immediately.
func (w *Worker) Drain() {
	w.draining.Store(true)
	w.drainCancelMu.Lock()
	cancel := w.drainCancel
	w.drainCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	w.sendHeartbeat(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sendHeartbeat(ctx)
		}
	}
}

func (w *Worker) sendHeartbeat(ctx context.Context) {
	hostname, _ := os.Hostname()
	hw := wkr.Worker{
		Name:          w.Name,
		Hostname:      hostname,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		Version:       w.Version,
		Commit:        w.Commit,
		Concurrency:   w.Concurrency,
		Tags:          w.Tags,
		ExclusiveTags: w.ExclusiveTags,
		StartedAt:     w.StartedAt,
	}
	if err := w.pikoci.WorkerHeartbeat(ctx, hw); err != nil {
		w.logger.Error("failed to send heartbeat", "error", err)
	}
}

// applyRunnerOverride resolves the runner for a type command, applying a
// runner override when present. When the override is set and the command
// uses "exec", the command is transformed to run under the override runner
// (e.g. docker) instead. Returns the resolved runner, the transformed
// command, and whether the runner was found.
func applyRunnerOverride(pp *pipeline.Pipeline, typeCmd *utils.RunnerCommand, override *utils.RunnerOverride) (runner.Runner, utils.RunnerCommand, bool) {
	runnerName := typeCmd.Runner
	rc := utils.RunnerCommand{
		Runner: typeCmd.Runner,
		Args:   make([]string, len(typeCmd.Args)),
		Params: make(map[string]string),
	}
	copy(rc.Args, typeCmd.Args)
	for k, v := range typeCmd.Params {
		rc.Params[k] = v
	}

	if override != nil && typeCmd.Runner == "exec" {
		runnerName = override.Runner
		rc.Runner = override.Runner
		for k, v := range override.Params {
			rc.Params[k] = v
		}
		// Transform exec path+args into cmd for target runner
		if path, ok := rc.Params["path"]; ok {
			cmd := path
			if len(typeCmd.Args) > 0 {
				cmd += " " + strings.Join(typeCmd.Args, " ")
			}
			rc.Params["cmd"] = cmd
			delete(rc.Params, "path")
		}
		// Override args replace exec args (used as extra runner flags, e.g. docker flags)
		if len(override.Args) > 0 {
			rc.Args = override.Args
		} else {
			rc.Args = nil
		}
	}

	ru, ok := pp.Runner(runnerName)
	return ru, rc, ok
}

// Run starts the worker event loop. When GRPCAddr is set, it connects via
// gRPC bidirectional streaming. Otherwise it uses HTTP long polling (embedded worker).
// Run blocks until the context is cancelled or an unrecoverable error occurs.
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("Worker waiting for messages...")

	// Store the parent context for DB operations that must survive
	// job-level cancellation but respect server shutdown.
	w.apiCtx = ctx

	// receiveCtx is cancelled on Drain() to unblock Receive() calls
	// immediately while still allowing in-flight jobs to finish.
	receiveCtx, receiveCancel := context.WithCancel(ctx)
	w.drainCancelMu.Lock()
	w.drainCancel = receiveCancel
	w.drainCancelMu.Unlock()
	defer receiveCancel()

	if w.GRPCAddr != "" {
		w.jobCancels = make(map[string]context.CancelFunc)
		return w.grpcLoop(receiveCtx)
	}

	// Embedded worker: use HTTP long polling + heartbeat loop
	go w.heartbeatLoop(ctx)
	return w.pollLoop(receiveCtx)
}

// pollLoop uses HTTP long polling to receive work items. It handles both
// job builds and resource checks through a single loop.
func (w *Worker) pollLoop(ctx context.Context) error {
	for {
		if w.draining.Load() {
			return nil
		}
		item, err := w.workPoller.PollNextWork(ctx, workitem.WorkerContext{
			Tags:          w.Tags,
			ExclusiveTags: w.ExclusiveTags,
		})
		if err != nil {
			if w.draining.Load() {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.logger.Error("poll error", "error", err)
			jitter := time.Duration(rand.Intn(5000)) * time.Millisecond
			time.Sleep(jitter)
			continue
		}
		if item == nil {
			continue
		}
		cwd, err := w.createWorkDir()
		if err != nil {
			return err
		}
		w.processMessage(ctx, item.Body, cwd)
		os.RemoveAll(cwd)
	}
}

func (w *Worker) createWorkDir() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to create UUID: %w", err)
	}
	// We append a file "pikoci" just so CacheFile creates the full dir,
	// afterward we just get the Dir of the cwd
	cwd, err := xdg.CacheFile(filepath.Join("pikoci", id.String(), "pikoci"))
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	return filepath.Dir(cwd), nil
}

func (w *Worker) processMessage(ctx context.Context, m workitem.Body, cwd string) {
	pp, err := w.pikoci.GetPipeline(ctx, m.TeamCanonical, m.PipelineCanonical)
	if err != nil {
		w.logger.Error("failed GetPipeline", "error", err)
		return
	}

	// Parse services from the pipeline's raw HCL since they are not stored
	// in a separate DB table.
	if len(pp.Raw) > 0 && len(pp.Services) == 0 {
		svcs, err := pipeline.ParseServicesFromRaw(ctx, pp.Raw)
		if err != nil {
			w.logger.Error("failed to parse services from pipeline raw", "error", err)
		} else {
			pp.Services = svcs
		}
	}

	// Parse secret-backed variables from the pipeline's raw HCL since they
	// are not stored in a separate DB table.
	if len(pp.Raw) > 0 && len(pp.SecretVars) == 0 {
		svars, err := pipeline.ParseSecretVarsFromRaw(pp.Raw, nil)
		if err != nil {
			w.logger.Error("failed to parse secret vars from pipeline raw", "error", err)
		} else {
			pp.SecretVars = svars
		}
	}

	if m.PipelineCanonical != "" && m.JobName != "" {
		w.processJob(ctx, m, cwd, pp)
	} else if m.PipelineCanonical != "" && m.ResourceCanonical != "" {
		w.processResourceCheck(ctx, m, cwd, pp)
	}
}

// processJob handles executing a job: transitions a pending build to started,
// runs the plan steps, and runs hooks. Downstream job triggering is handled
// by the scheduler.
func (w *Worker) processJob(ctx context.Context, m workitem.Body, cwd string, pp *pipeline.Pipeline) {
	if m.BuildID == 0 {
		w.logger.Error("missing build_id in message",
			"pipeline", m.PipelineCanonical, "job", m.JobName)
		return
	}

	w.logger.Info("processJob called",
		"pipeline", m.PipelineCanonical, "job", m.JobName, "build_id", m.BuildID,
		"version_id", m.VersionID, "resource", m.ResourceCanonical)

	var nb *build.Build

	if m.BuildNumber != "" {
		// Build was already started by NextWork (poll-based flow).
		// Retrieve the started build directly.
		var err error
		nb, err = w.pikoci.GetJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildNumber)
		if err != nil {
			w.logger.Error("failed to get already-started build",
				"pipeline", m.PipelineCanonical, "job", m.JobName, "build_number", m.BuildNumber, "error", err)
			return
		}
	} else if w.LocalMode {
		// Local mode: start the build here.
		var err error
		nb, err = w.pikoci.StartPendingBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID)
		if err != nil {
			w.logger.Error("failed to start pending build",
				"pipeline", m.PipelineCanonical, "job", m.JobName, "build_id", m.BuildID, "error", err)
			return
		}
	} else {
		w.logger.Error("build_number not set and not in local mode",
			"pipeline", m.PipelineCanonical, "job", m.JobName, "build_id", m.BuildID)
		return
	}

	b := build.Build{
		ID:                nb.ID,
		BuildNumber:       nb.BuildNumber,
		Status:            build.Started,
		StartedAt:         nb.StartedAt,
		Steps:             []build.Step{},
		VersionID:         nb.VersionID,
		ResourceCanonical: nb.ResourceCanonical,
	}

	// If the message doesn't carry version info, fall back to what was stored on the build
	if m.VersionID == 0 && nb.VersionID != 0 {
		m.VersionID = nb.VersionID
	}
	if m.ResourceCanonical == "" && nb.ResourceCanonical != "" {
		m.ResourceCanonical = nb.ResourceCanonical
	}

	// After the build completes (success, failure, or cancellation),
	// notify the next pending build so it can start.
	defer w.notifyNextPendingBuild(ctx, m)

	w.logger.Info("build started",
		"pipeline", m.PipelineCanonical, "job", m.JobName, "build_number", b.BuildNumber,
		"build_id", b.ID, "version_id", m.VersionID)

	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()

	// Poll for cancellation in background (only for non-gRPC mode;
	// gRPC workers receive CancelJob messages over the stream).
	if w.GRPCAddr == "" {
		go w.pollForCancellation(ctx, jobCtx, jobCancel, m, b.BuildNumber)
	}

	j, err := w.pikoci.GetPipelineJob(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName)
	if err != nil {
		w.failBuild(ctx, m, b, fmt.Errorf("failed to get job: %w", err))
		return
	}

	if j.Timeout > 0 {
		var timeoutCancel context.CancelFunc
		jobCtx, timeoutCancel = context.WithTimeout(jobCtx, j.Timeout)
		defer timeoutCancel()
	}

	var resolvedVersions map[string]uint32
	if w.LocalMode {
		resolvedVersions = make(map[string]uint32)
	} else if m.RetryBuildNumber != "" && m.RetryBuildID != 0 {
		// Look up versions from the retried build
		stepVersions, err := w.pikoci.FindBuildGetVersions(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, m.RetryBuildID)
		if err != nil {
			w.failBuild(ctx, m, b, fmt.Errorf("failed to find retry build versions: %w", err))
			return
		}
		// Convert step_name keys to resource_canonical keys
		resolvedVersions = make(map[string]uint32)
		for _, ps := range j.FlatPlanSteps() {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			if vid, ok := stepVersions[ps.Get.Name]; ok {
				resolvedVersions[ps.Get.ResourceCanonical()] = vid
			}
		}
	} else {
		ok, rv := w.checkPassedConstraints(jobCtx, m, &b, j, pp)
		if !ok {
			return
		}
		resolvedVersions = rv

		// checkVersionAvailability verifies that the get step can pull a version.
		// If no version is available (e.g. manual trigger with no resource versions),
		// the build is deleted silently — no hooks run, no failure recorded.
		if !w.checkVersionAvailability(jobCtx, m, &b, j, pp) {
			return
		}
	}

	failed, resolved, exportedVars := w.runPlan(jobCtx, m, &b, cwd, pp, j, resolvedVersions)

	// Handle job-level timeout
	if jobCtx.Err() == context.DeadlineExceeded {
		w.failBuild(ctx, m, b, fmt.Errorf("job timed out after %s", j.Timeout))
		w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnCancel, "on_cancel", resolved, exportedVars, "cancelled")
		w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.Ensure, "ensure", resolved, exportedVars, "cancelled")
		w.runAutoNotifications(ctx, m, &b, cwd, pp, resolved)
		return
	}

	// Handle user-initiated cancellation
	if jobCtx.Err() == context.Canceled {
		current, err := w.pikoci.GetJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.BuildNumber)
		if err == nil && current.Status == build.Cancelled {
			b.Status = build.Cancelled
			w.updateBuild(ctx, m, b)
			w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnCancel, "on_cancel", resolved, exportedVars, "cancelled")
			w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.Ensure, "ensure", resolved, exportedVars, "cancelled")
			w.runAutoNotifications(ctx, m, &b, cwd, pp, resolved)
			return
		}
	}

	if !failed {
		b.Status = build.Succeeded
		if err := w.updateBuild(ctx, m, b); err != nil {
			return
		}
		// Trigger downstream jobs with passed constraints immediately
		if err := w.pikoci.EvaluateDownstreamJobs(ctx, m.TeamCanonical, m.PipelineCanonical, j.Name); err != nil {
			w.logger.Error("failed to evaluate downstream jobs",
				"pipeline", m.PipelineCanonical, "job", j.Name, "error", err)
		}
		w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnSuccess, "on_success", resolved, exportedVars, "succeeded")
	} else {
		// Ensure local b reflects the Failed status set by failBuild (which
		// operates on a copy) so that any subsequent updateBuild calls from
		// hooks don't overwrite the DB status back to Started.
		if b.Status != build.Failed {
			b.Status = build.Failed
		}
		w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnFailure, "on_failure", resolved, exportedVars, "failed")
	}
	status := "succeeded"
	if b.Status == build.Failed {
		status = "failed"
	}
	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.Ensure, "ensure", resolved, exportedVars, status)

	// Fire automatic notifications based on build status
	w.runAutoNotifications(ctx, m, &b, cwd, pp, resolved)
}

func (w *Worker) notifyNextPendingBuild(ctx context.Context, m workitem.Body) {
	// Notify serial-group peers and the same job's pending builds.
	// The server-side NotifySerialGroupPendingBuilds and
	// sendPendingBuildNotification both call Notifier.Notify(),
	// which wakes up any polling workers.
	w.pikoci.NotifySerialGroupPendingBuilds(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName)
}

// checkPassedConstraints verifies that all jobs in the "passed" list have a
// successful build that used a common resource version. The returned map
// contains resourceCanonical → resolvedVersionID for each get step with Passed.
// If no common version exists, the build is deleted and (false, nil) is returned.
// For for_each jobs, group names in "passed" are expanded to all instance names.
func (w *Worker) checkPassedConstraints(ctx context.Context, m workitem.Body, b *build.Build, j *job.Job, pp *pipeline.Pipeline) (bool, map[string]uint32) {
	resolvedVersions := make(map[string]uint32)
	for _, ps := range j.FlatPlanSteps() {
		if ps.Type != job.StepTypeGet || ps.Get == nil {
			continue
		}
		g := ps.Get
		if len(g.Passed) == 0 {
			continue
		}
		rCan := g.ResourceCanonical()

		// Expand for_each group names to instance names
		expandedPassed := resolvePassedJobNames(g.Passed, pp)

		var intersection map[uint32]bool
		var hasSucceeded bool
		for _, p := range expandedPassed {
			builds, _, err := w.pikoci.ListJobBuilds(ctx, m.TeamCanonical, m.PipelineCanonical, p, nil, nil, 0)
			if err != nil {
				w.failBuild(ctx, m, *b, fmt.Errorf("failed to list builds for passed job %q: %w", p, err))
				return false, nil
			}

			// Collect version IDs from successful builds where a get step matches this resource
			versionSet := make(map[uint32]bool)
			for _, bu := range builds {
				if bu.Status != build.Succeeded {
					continue
				}
				hasSucceeded = true
				for _, step := range bu.Steps {
					if (step.Type == "get" || step.Type == "put") && step.Name == g.Name && step.VersionID != 0 {
						versionSet[step.VersionID] = true
					}
				}
			}

			if intersection == nil {
				intersection = versionSet
			} else {
				for vid := range intersection {
					if !versionSet[vid] {
						delete(intersection, vid)
					}
				}
			}
		}

		if len(intersection) == 0 {
			if hasSucceeded {
				w.logger.Info("job will not run: no common version across passed jobs",
					"job", m.JobName, "pipeline", m.PipelineCanonical, "resource", rCan)
			} else {
				w.logger.Info("job will not run: no successful builds in passed jobs",
					"job", m.JobName, "pipeline", m.PipelineCanonical, "resource", rCan)
			}
			w.deleteBuild(ctx, m, *b)
			return false, nil
		}

		// Pick the highest version ID (newest)
		var best uint32
		for vid := range intersection {
			if vid > best {
				best = vid
			}
		}
		resolvedVersions[rCan] = best
	}
	return true, resolvedVersions
}

// resolvePassedJobNames expands for_each group names in a passed list to all
// instance names. If a name matches a for_each group, it is replaced by all
// instance names in that group. Non-group names are kept as-is.
func resolvePassedJobNames(passed []string, pp *pipeline.Pipeline) []string {
	if pp == nil {
		return passed
	}
	// Build a map of group name -> instance names
	groupInstances := make(map[string][]string)
	for _, j := range pp.Jobs {
		if j.ForEachGroup != "" {
			groupInstances[j.ForEachGroup] = append(groupInstances[j.ForEachGroup], j.Name)
		}
	}

	var expanded []string
	for _, name := range passed {
		if instances, ok := groupInstances[name]; ok {
			expanded = append(expanded, instances...)
		} else {
			expanded = append(expanded, name)
		}
	}
	return expanded
}

// checkVersionAvailability verifies that all get steps in the plan have a
// version available to pull. If any get step has no version, the build is
// deleted and false is returned (same behavior as checkPassedConstraints).
// This prevents hooks from running when no work can be done.
func (w *Worker) checkVersionAvailability(ctx context.Context, m workitem.Body, b *build.Build, j *job.Job, pp *pipeline.Pipeline) bool {
	for _, ps := range j.FlatPlanSteps() {
		if ps.Type != job.StepTypeGet || ps.Get == nil {
			continue
		}
		g := ps.Get
		rCan := g.ResourceCanonical()
		r, ok := pp.Resource(rCan)
		if !ok {
			w.logger.Warn("get step references unknown resource", "resource", rCan, "job", m.JobName)
			continue
		}

		dbvers, _, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, nil, nil, 0)
		if err != nil {
			// Transient errors (DB, network) should fail the build, not silently delete it.
			w.failBuild(ctx, m, *b, fmt.Errorf("failed to list resource versions: %w", err))
			return false
		}

		if len(dbvers) == 0 {
			w.logger.Info("job will not run: no versions available",
				"job", m.JobName, "pipeline", m.PipelineCanonical, "resource", r.Canonical)
			w.deleteBuild(ctx, m, *b)
			return false
		}
	}
	return true
}

// runPlan runs all plan steps (service/get/task/put) in declaration order.
// Services are started when their position in the plan is reached and stopped
// unconditionally after the plan completes (or fails).
// Returns true if the job failed during plan execution.
func (w *Worker) runPlan(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, j *job.Job, resolvedVersions map[string]uint32) (bool, map[string]string, map[string]string) {
	// Track all started services so we can stop them at the end.
	var allStartedServices []job.ServiceStep
	defer func() {
		if len(allStartedServices) > 0 {
			w.stopServices(m, b, cwd, pp, allStartedServices)
		}
	}()

	// Resolve secret-backed variables once for the entire job execution.
	resolved, err := w.resolveSecretVars(ctx, cwd, pp)
	if err != nil {
		w.failBuild(ctx, m, *b, fmt.Errorf("failed to resolve secret vars: %w", err))
		return true, nil, nil
	}

	secretVals := secretValuesFromResolved(resolved)

	// exportedVars accumulates key-value pairs exported by get and task steps
	// so that subsequent steps can consume them as environment variables.
	exportedVars := make(map[string]string)

	// Run plan steps in declaration order
	for _, ps := range j.Plan {
		switch ps.Type {
		case job.StepTypeService:
			if ps.Service == nil {
				continue
			}
			// Collect consecutive service steps and start them as a batch
			batch := []job.ServiceStep{*ps.Service}
			startedServices := w.startServices(ctx, m, b, cwd, pp, batch)
			allStartedServices = append(allStartedServices, startedServices...)
			if len(startedServices) != len(batch) {
				return true, resolved, exportedVars
			}
			if !w.waitForServices(ctx, m, b, cwd, pp, startedServices) {
				return true, resolved, exportedVars
			}
		case job.StepTypeGet:
			if ps.Get == nil {
				continue
			}
			if w.runGetStep(ctx, m, b, cwd, pp, *ps.Get, ps, resolvedVersions, exportedVars, secretVals, resolved) {
				return true, resolved, exportedVars
			}
		case job.StepTypeTask:
			if ps.Task == nil {
				continue
			}
			if w.runTaskStep(ctx, m, b, cwd, pp, *ps.Task, ps, exportedVars, secretVals, resolved) {
				return true, resolved, exportedVars
			}
		case job.StepTypePut:
			if ps.Put == nil {
				continue
			}
			if w.runPutStep(ctx, m, b, cwd, pp, *ps.Put, ps, exportedVars, secretVals, resolved) {
				return true, resolved, exportedVars
			}
		case job.StepTypeNotify:
			if ps.Notify == nil {
				continue
			}
			if w.runNotifyStep(ctx, m, b, cwd, pp, *ps.Notify, ps, exportedVars, secretVals, resolved) {
				return true, resolved, exportedVars
			}
		case job.StepTypeInParallel:
			if ps.InParallel == nil {
				continue
			}
			if w.runInParallelStep(ctx, m, b, cwd, pp, *ps.InParallel, ps, resolvedVersions, exportedVars, secretVals, resolved) {
				return true, resolved, exportedVars
			}
		}
	}
	return false, resolved, exportedVars
}

// runInParallelStep runs inner steps concurrently.
// Returns true if the parallel group failed.
func (w *Worker) runInParallelStep(
	ctx context.Context, m workitem.Body, b *build.Build,
	cwd string, pp *pipeline.Pipeline, ip job.InParallelStep,
	ps job.PlanStep, resolvedVersions map[string]uint32,
	exportedVars map[string]string, secretVals []string, resolved map[string]string,
) bool {
	start := time.Now()

	// Empty block is a no-op
	if len(ip.Steps) == 0 {
		b.Steps = append(b.Steps, build.Step{
			Type: "in_parallel", Status: build.Succeeded, Duration: 0,
		})
		w.updateBuild(ctx, m, *b)
		return false
	}

	// Add parent step placeholder
	parentIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{
		Type: "in_parallel", Status: build.Started,
	})
	w.updateBuild(ctx, m, *b)

	// Snapshot exportedVars (read-only base for all goroutines)
	snapshotVars := make(map[string]string, len(exportedVars))
	for k, v := range exportedVars {
		snapshotVars[k] = v
	}

	// Shared cancellation context for fail_fast
	parallelCtx, cancelParallel := context.WithCancel(ctx)
	defer cancelParallel()

	// Semaphore for limit
	var sem chan struct{}
	if ip.Limit > 0 {
		sem = make(chan struct{}, ip.Limit)
	}

	type parallelResult struct {
		exportedVars map[string]string
		failed       bool
	}

	results := make([]parallelResult, len(ip.Steps))
	var wg sync.WaitGroup
	var mu sync.Mutex // protects b and updateBuild calls

	// syncSubSteps copies localBuild.Steps into the parent's SubSteps at the
	// goroutine's reserved offset, then persists the real build.
	// Called under mu by each goroutine whenever its local state changes.
	type subStepSlot struct {
		steps []build.Step
	}
	slots := make([]subStepSlot, len(ip.Steps))
	// Pre-populate slots with pending steps so the UI shows queued tasks
	for i, innerPS := range ip.Steps {
		var name, typ string
		switch innerPS.Type {
		case job.StepTypeGet:
			if innerPS.Get != nil {
				name, typ = innerPS.Get.Name, "get"
			}
		case job.StepTypeTask:
			if innerPS.Task != nil {
				name, typ = innerPS.Task.Name, "task"
			}
		case job.StepTypePut:
			if innerPS.Put != nil {
				name, typ = innerPS.Put.Name, "put"
			}
		case job.StepTypeNotify:
			if innerPS.Notify != nil {
				name, typ = innerPS.Notify.Name, "notify"
			}
		}
		slots[i] = subStepSlot{steps: []build.Step{{Type: typ, Name: name, Status: build.Pending}}}
	}
	rebuildSubSteps := func() {
		var merged []build.Step
		for _, sl := range slots {
			merged = append(merged, sl.steps...)
		}
		b.Steps[parentIdx].SubSteps = merged
		w.updateBuild(ctx, m, *b)
	}
	rebuildSubSteps() // persist initial pending state

	for i, innerPS := range ip.Steps {
		wg.Add(1)
		go func(idx int, ps job.PlanStep) {
			defer wg.Done()

			// Acquire semaphore
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-parallelCtx.Done():
					results[idx] = parallelResult{failed: true}
					return
				}
			}

			// Check if cancelled before starting
			if parallelCtx.Err() != nil {
				results[idx] = parallelResult{failed: true}
				return
			}

			// Per-goroutine state
			localVars := make(map[string]string, len(snapshotVars))
			for k, v := range snapshotVars {
				localVars[k] = v
			}

			// Create a local build that syncs its steps to the parent on every update.
			localBuild := &build.Build{SuppressUpdates: true}
			localBuild.OnUpdate = func() {
				mu.Lock()
				defer mu.Unlock()
				slots[idx].steps = make([]build.Step, len(localBuild.Steps))
				copy(slots[idx].steps, localBuild.Steps)
				rebuildSubSteps()
			}

			var failed bool
			switch ps.Type {
			case job.StepTypeGet:
				if ps.Get != nil {
					failed = w.runGetStep(parallelCtx, m, localBuild, cwd, pp, *ps.Get, ps, resolvedVersions, localVars, secretVals, resolved)
				}
			case job.StepTypeTask:
				if ps.Task != nil {
					failed = w.runTaskStep(parallelCtx, m, localBuild, cwd, pp, *ps.Task, ps, localVars, secretVals, resolved)
				}
			case job.StepTypePut:
				if ps.Put != nil {
					failed = w.runPutStep(parallelCtx, m, localBuild, cwd, pp, *ps.Put, ps, localVars, secretVals, resolved)
				}
			case job.StepTypeNotify:
				if ps.Notify != nil {
					failed = w.runNotifyStep(parallelCtx, m, localBuild, cwd, pp, *ps.Notify, ps, localVars, secretVals, resolved)
				}
			}

			results[idx] = parallelResult{
				exportedVars: localVars,
				failed:       failed,
			}

			// Final sync
			mu.Lock()
			slots[idx].steps = make([]build.Step, len(localBuild.Steps))
			copy(slots[idx].steps, localBuild.Steps)
			rebuildSubSteps()
			mu.Unlock()

			if failed && ip.FailFast {
				cancelParallel()
			}
		}(i, innerPS)
	}

	wg.Wait()

	// Aggregate results
	anyFailed := false
	for _, r := range results {
		if r.failed {
			anyFailed = true
		}
		// Merge new exported vars (keys added by this step)
		for k, v := range r.exportedVars {
			if _, exists := snapshotVars[k]; !exists {
				exportedVars[k] = v
			}
		}
	}

	// Update parent step with final status
	status := build.Succeeded
	if anyFailed {
		status = build.Failed
		b.Status = build.Failed
	}
	b.Steps[parentIdx].Status = status
	b.Steps[parentIdx].Duration = time.Since(start)
	w.updateBuild(ctx, m, *b)

	// Run in_parallel-level hooks
	if anyFailed {
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, "", ps.OnFailure, "on_failure", resolved, exportedVars)
	} else {
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, "", ps.OnSuccess, "on_success", resolved, exportedVars)
	}
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, "", ps.Ensure, "ensure", resolved, exportedVars)

	return anyFailed
}

// runGetStep runs a single get step (resource pull).
// Returns true if the step failed.
func (w *Worker) runGetStep(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, g job.GetStep, ps job.PlanStep, resolvedVersions map[string]uint32, exportedVars map[string]string, secretVals []string, resolved ...map[string]string) bool {
	var secretResolved map[string]string
	if len(resolved) > 0 {
		secretResolved = resolved[0]
	}
	rCan := g.ResourceCanonical()

	if w.ResourceOverrides != nil {
		if localPath, ok := w.ResourceOverrides[rCan]; ok {
			return w.runGetStepLocal(ctx, m, b, cwd, pp, g, ps, localPath)
		}
	}
	r, ok := pp.Resource(rCan)
	if !ok {
		return false
	}
	rt, ok := pp.ResourceType(g.Type)
	if !ok {
		return false
	}

	if rt.Pull == nil {
		return false
	}

	var passedVersionID uint32
	if resolvedVersions != nil {
		passedVersionID = resolvedVersions[rCan]
	}

	params, usedVersionID, pullWarnings := w.buildPullParams(ctx, m, b, rt, r, g, passedVersionID)
	if params == nil {
		return true
	}

	ru, rc, ok := applyRunnerOverride(pp, rt.Pull, rt.Runner)
	if !ok {
		return false
	}

	for k, v := range params {
		rc.Params[k] = v
	}
	for k, v := range buildMetadataParams(b, m) {
		rc.Params[k] = v
	}
	for k, v := range exportedVars {
		rc.Params[k] = v
	}
	if rt.Runner != nil {
		delete(rc.Params, "path")
	}

	if resourceCacheEnabled(rt, r) {
		cd, err := w.cacheDir(m.TeamCanonical, m.PipelineCanonical, rCan)
		if err != nil {
			w.logger.Error("failed to create cache dir for pull", "error", err)
		} else {
			rc.Params["CACHE_DIR"] = cd
		}
	}

	replaceSecretPlaceholders(rc.Params, secretResolved)
	replaceSecretPlaceholdersInSlice(rc.Args, secretResolved)

	maxAttempts := ps.Attempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	// Append a "running" step and persist it
	stepIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{Type: "get", Name: g.Name, Status: build.Started})
	w.updateBuild(ctx, m, *b)

	out := formatParamWarnings(pullWarnings)
	var d time.Duration
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && maxAttempts > 1 {
			out += fmt.Sprintf("\n--- attempt %d/%d ---\n", attempt, maxAttempts)
		}

		prefix := out
		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = prefix + partial
			w.updateBuild(ctx, m, *b)
		}

		runCtx := ctx
		var cancel context.CancelFunc
		if ps.Timeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, ps.Timeout)
		}

		var attemptOut string
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, rc, secretVals, onPartialLog)
		out += attemptOut

		if cancel != nil {
			cancel()
		}

		if err == nil {
			break
		}

		if runCtx.Err() == context.DeadlineExceeded {
			out += fmt.Sprintf("\nstep timed out after %s", ps.Timeout)
		}
	}

	if err != nil {
		b.Steps[stepIdx] = build.Step{Type: "get", Name: g.Name, Logs: out, Duration: d, Status: build.Failed}
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		w.logger.Error("failed to run get step", "step", g.Name, "error", err)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.OnFailure, "on_failure", secretResolved, exportedVars)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
		return true
	}

	b.Steps[stepIdx] = build.Step{
		Type:      "get",
		Name:      g.Name,
		VersionID: usedVersionID,
		Logs:      out,
		Duration:  d,
		Status:    build.Succeeded,
	}
	if err := w.updateBuild(ctx, m, *b); err != nil {
		return true
	}

	if usedVersionID != 0 {
		if err := w.pikoci.InsertBuildGetVersion(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.ID, g.Name, usedVersionID); err != nil {
			w.logger.Error("failed to insert build get version", "step", g.Name, "error", err)
		}
	}

	// Export version_* params as GET_<STEPNAME>_<KEY> for subsequent steps.
	prefix := "GET_" + sanitizeStepName(g.Name) + "_"
	for k, v := range params {
		if strings.HasPrefix(k, "version_") {
			exportedVars[prefix+strings.ToUpper(strings.TrimPrefix(k, "version_"))] = v
		}
	}

	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.OnSuccess, "on_success", secretResolved, exportedVars)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
	return false
}

// runGetStepLocal handles a get step by copying a local directory into the
// working directory instead of running the resource type's pull command. This
// is used for local execution with --resource overrides. A real copy is used
// instead of a symlink because symlinks break with Docker volume mounts (the
// container can't resolve host-side symlink targets).
// Returns true if the step failed.
func (w *Worker) runGetStepLocal(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, g job.GetStep, ps job.PlanStep, localPath string) bool {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		b.Steps = append(b.Steps, build.Step{Type: "get", Name: g.Name, Logs: fmt.Sprintf("failed to resolve path %q: %s", localPath, err), Status: build.Failed})
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		return true
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		b.Steps = append(b.Steps, build.Step{Type: "get", Name: g.Name, Logs: fmt.Sprintf("local resource override path does not exist: %s", absPath), Status: build.Failed})
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		return true
	}

	// Use the resource's "name" param as the directory name if available,
	// matching the behavior of resource type pull commands (e.g. git clone
	// into $param_name). Fall back to the step name.
	dirName := g.Name
	if pp != nil {
		rCan := g.ResourceCanonical()
		if r, ok := pp.Resource(rCan); ok && r.Params != nil {
			if n, ok := r.Params.Params["name"]; ok && n != "" {
				dirName = n
			}
		}
	}

	dst := filepath.Join(cwd, dirName)
	// Use cp -a to copy preserving permissions. This works with all runners
	// including Docker, unlike symlinks which break across mount boundaries.
	cpCmd := exec.CommandContext(ctx, "cp", "-a", absPath, dst)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		b.Steps = append(b.Steps, build.Step{Type: "get", Name: g.Name, Logs: fmt.Sprintf("failed to copy %s -> %s: %s\n%s", absPath, dst, err, string(out)), Status: build.Failed})
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		return true
	}

	logMsg := fmt.Sprintf("using local resource override: %s -> %s", absPath, dirName)
	b.Steps = append(b.Steps, build.Step{Type: "get", Name: g.Name, Logs: logMsg, Status: build.Succeeded})
	w.updateBuild(ctx, m, *b)
	w.runHooks(ctx, m, b, &b.Steps, cwd, nil, g.Name, ps.OnSuccess, "on_success", nil, nil)
	w.runHooks(ctx, m, b, &b.Steps, cwd, nil, g.Name, ps.Ensure, "ensure", nil, nil)
	return false
}

// runTaskStep runs a single task step.
// Returns true if the step failed.
func (w *Worker) runTaskStep(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, t job.TaskStep, ps job.PlanStep, exportedVars map[string]string, secretVals []string, resolved ...map[string]string) bool {
	var secretResolved map[string]string
	if len(resolved) > 0 {
		secretResolved = resolved[0]
	}
	ru, ok := pp.Runner(t.Run.Runner)
	if !ok {
		return false
	}

	if t.Run.Params == nil {
		t.Run.Params = make(map[string]string)
	}

	taskWarnings := validateTaskRunParams(t.Run.Params, w.logger, t.Name)

	replaceSecretPlaceholders(t.Run.Params, secretResolved)
	replaceSecretPlaceholdersInSlice(t.Run.Args, secretResolved)

	for k, v := range buildMetadataParams(b, m) {
		t.Run.Params[k] = v
	}
	for k, v := range exportedVars {
		t.Run.Params[k] = v
	}

	// Create the PIKOCI_OUTPUT file inside cwd so Docker-mounted tasks can write to it.
	if outputFile, oerr := os.CreateTemp(cwd, ".pikoci-output-*"); oerr != nil {
		w.logger.Error("failed to create PIKOCI_OUTPUT file", "error", oerr)
	} else {
		outputPath := outputFile.Name()
		outputFile.Close()
		defer os.Remove(outputPath)
		t.Run.Params["PIKOCI_OUTPUT"] = outputPath
	}

	for _, input := range t.Inputs {
		if _, err := os.Stat(filepath.Join(cwd, input)); err != nil {
			errMsg := fmt.Sprintf("input %q does not exist", input)
			b.Steps = append(b.Steps, build.Step{Type: "task", Name: t.Name, Logs: errMsg, Status: build.Failed})
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, nil)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnFailure, "on_failure", secretResolved, exportedVars)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
			return true
		}
	}

	maxAttempts := ps.Attempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	// Append a "running" step and persist it
	stepIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{Type: "task", Name: t.Name, Status: build.Started})
	w.updateBuild(ctx, m, *b)

	out := formatParamWarnings(taskWarnings)
	var d time.Duration
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && maxAttempts > 1 {
			out += fmt.Sprintf("\n--- attempt %d/%d ---\n", attempt, maxAttempts)
		}

		prefix := out
		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = prefix + partial
			w.updateBuild(ctx, m, *b)
		}

		runCtx := ctx
		var cancel context.CancelFunc
		if ps.Timeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, ps.Timeout)
		}

		var attemptOut string
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, t.Run, secretVals, onPartialLog)
		out += attemptOut

		if cancel != nil {
			cancel()
		}

		if err == nil {
			break
		}

		if runCtx.Err() == context.DeadlineExceeded {
			out += fmt.Sprintf("\nstep timed out after %s", ps.Timeout)
		}
	}

	if err != nil {
		b.Steps[stepIdx] = build.Step{Type: "task", Name: t.Name, Logs: out, Duration: d, Status: build.Failed}
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnFailure, "on_failure", secretResolved, exportedVars)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
		return true
	}

	b.Steps[stepIdx] = build.Step{Type: "task", Name: t.Name, Logs: out, Duration: d, Status: build.Succeeded}

	for _, output := range t.Outputs {
		if _, err := os.Stat(filepath.Join(cwd, output)); err != nil {
			errMsg := fmt.Sprintf("task finished but output %q was not produced", output)
			b.Steps[stepIdx] = build.Step{Type: "task", Name: t.Name, Logs: out + "\n" + errMsg, Duration: d, Status: build.Failed}
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, nil)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnFailure, "on_failure", secretResolved, exportedVars)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
			return true
		}
	}

	// Parse PIKOCI_OUTPUT and export values for subsequent steps.
	// Done after output validation so failed steps don't pollute exportedVars.
	if outputPath, ok := t.Run.Params["PIKOCI_OUTPUT"]; ok {
		parsed := parseOutputFile(outputPath, w.logger)
		prefix := "TASK_" + sanitizeStepName(t.Name) + "_"
		for k, v := range parsed {
			exportedVars[prefix+k] = v
		}
	}

	if err := w.updateBuild(ctx, m, *b); err != nil {
		return true
	}
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnSuccess, "on_success", secretResolved, exportedVars)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
	return false
}

// runPutStep runs a single put step (resource push).
// Returns true if the step failed.
func (w *Worker) runPutStep(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, p job.PutStep, ps job.PlanStep, exportedVars map[string]string, secretVals []string, resolved ...map[string]string) bool {
	if w.LocalMode {
		rCan := utils.ResourceCanonical(p.Type, p.Name)
		logMsg := fmt.Sprintf("skipping put step (local execution): %s", rCan)
		b.Steps = append(b.Steps, build.Step{Type: "put", Name: p.Name, Logs: logMsg, Status: build.Succeeded})
		w.updateBuild(ctx, m, *b)
		return false
	}

	var secretResolved map[string]string
	if len(resolved) > 0 {
		secretResolved = resolved[0]
	}
	rCan := utils.ResourceCanonical(p.Type, p.Name)
	r, ok := pp.Resource(rCan)
	if !ok {
		return false
	}
	rt, ok := pp.ResourceType(p.Type)
	if !ok {
		return false
	}

	if rt.Source == "pikoci://trigger" {
		return w.runPutStepTrigger(ctx, m, b, pp, p, ps)
	}

	if rt.Push == nil {
		return false
	}

	params := make(map[string]string)
	for k, v := range rt.Push.Params {
		params[k] = v
	}
	// Add resource-level params
	accepted, putWarnings := validateParams(r.GetParams(), rt.Params, "param_", w.logger, "resource_type", rt.Name)
	for k, v := range accepted {
		params[k] = v
	}
	// Add put-step-level params with put_ prefix
	for k, v := range p.Params {
		params["put_"+k] = v
	}
	for k, v := range buildMetadataParams(b, m) {
		params[k] = v
	}
	for k, v := range exportedVars {
		params[k] = v
	}

	ru, rc, ok := applyRunnerOverride(pp, rt.Push, rt.Runner)
	if !ok {
		return false
	}

	for k, v := range params {
		rc.Params[k] = v
	}
	if rt.Runner != nil {
		delete(rc.Params, "path")
	}

	if resourceCacheEnabled(rt, r) {
		cd, err := w.cacheDir(m.TeamCanonical, m.PipelineCanonical, rCan)
		if err != nil {
			w.logger.Error("failed to create cache dir for push", "error", err)
		} else {
			rc.Params["CACHE_DIR"] = cd
		}
	}

	replaceSecretPlaceholders(rc.Params, secretResolved)
	replaceSecretPlaceholdersInSlice(rc.Args, secretResolved)

	maxAttempts := ps.Attempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	// Append a "running" step and persist it
	stepIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{Type: "put", Name: p.Name, Status: build.Started})
	w.updateBuild(ctx, m, *b)

	out := formatParamWarnings(putWarnings)
	var d time.Duration
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && maxAttempts > 1 {
			out += fmt.Sprintf("\n--- attempt %d/%d ---\n", attempt, maxAttempts)
		}

		prefix := out
		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = prefix + partial
			w.updateBuild(ctx, m, *b)
		}

		runCtx := ctx
		var cancel context.CancelFunc
		if ps.Timeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, ps.Timeout)
		}

		var attemptOut string
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, rc, secretVals, onPartialLog)
		out += attemptOut

		if cancel != nil {
			cancel()
		}

		if err == nil {
			break
		}

		if runCtx.Err() == context.DeadlineExceeded {
			out += fmt.Sprintf("\nstep timed out after %s", ps.Timeout)
		}
	}

	if err != nil {
		b.Steps[stepIdx] = build.Step{Type: "put", Name: p.Name, Logs: out, Duration: d, Status: build.Failed}
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		w.logger.Error("failed to run put step", "step", p.Name, "error", err)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.OnFailure, "on_failure", secretResolved, exportedVars)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
		return true
	}

	b.Steps[stepIdx] = build.Step{Type: "put", Name: p.Name, Logs: out, Duration: d, Status: build.Succeeded}
	if err := w.updateBuild(ctx, m, *b); err != nil {
		return true
	}

	// Implicit get-after-put: parse the version from the push script output,
	// create it in the DB, record it as fetched by this build, and trigger
	// downstream jobs. This mirrors Concourse's implicit get after put.
	w.implicitGetAfterPut(ctx, m, b, pp, p, rCan, r, out, stepIdx)

	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.OnSuccess, "on_success", secretResolved, exportedVars)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
	return false
}

// implicitGetAfterPut parses the version JSON from the push script output,
// creates the resource version in the DB, records it as fetched by this build
// (so that "passed" constraints are satisfied), and triggers downstream jobs.
func (w *Worker) implicitGetAfterPut(ctx context.Context, m workitem.Body, b *build.Build, pp *pipeline.Pipeline, p job.PutStep, rCan string, r resource.Resource, out string, stepIdx int) {
	sout := strings.Split(strings.Trim(out, "\n"), "\n")
	rawVers := sout[len(sout)-1]
	if rawVers == "" {
		return
	}

	vers := make([]map[string]interface{}, 0)
	if err := json.Unmarshal([]byte(rawVers), &vers); err != nil {
		w.logger.Warn("put step output is not valid version JSON, skipping implicit get", "step", p.Name, "error", err)
		return
	}

	for _, v := range vers {
		cv, err := w.pikoci.CreateResourceVersion(ctx, m.TeamCanonical, m.PipelineCanonical, rCan, resource.Version{
			Version: v,
		})
		if err != nil {
			if isDuplicateKeyError(err) {
				w.logger.Info("put: duplicate version skipped", "resource", rCan, "version", v)
				continue
			}
			w.logger.Error("put: failed to create resource version", "resource", rCan, "error", err)
			return
		}

		// Record the version on the put step so checkPassedConstraints can find it
		b.Steps[stepIdx] = build.Step{
			Type:      b.Steps[stepIdx].Type,
			Name:      b.Steps[stepIdx].Name,
			Logs:      b.Steps[stepIdx].Logs,
			Duration:  b.Steps[stepIdx].Duration,
			Status:    b.Steps[stepIdx].Status,
			VersionID: cv.ID,
		}
		w.updateBuild(ctx, m, *b)

		if err := w.pikoci.InsertBuildGetVersion(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.ID, p.Name, cv.ID); err != nil {
			w.logger.Error("put: failed to insert build get version", "step", p.Name, "error", err)
		}

		w.logger.Info("put: version created, triggering downstream jobs",
			"pipeline", m.PipelineCanonical, "resource", rCan, "version_id", cv.ID)
		w.triggerResourceJobs(ctx, m, pp, r, cv)
	}
}

// runNotifyStep runs a single notify step (fire-and-forget notification).
// Returns true if the step failed.
func (w *Worker) runNotifyStep(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, n job.NotifyStep, ps job.PlanStep, exportedVars map[string]string, secretVals []string, resolved ...map[string]string) bool {
	if w.LocalMode {
		nCan := utils.NotificationCanonical(n.Type, n.Name)
		logMsg := fmt.Sprintf("skipping notify step (local execution): %s", nCan)
		b.Steps = append(b.Steps, build.Step{Type: "notify", Name: n.Name, Logs: logMsg, Status: build.Succeeded})
		w.updateBuild(ctx, m, *b)
		return false
	}

	var secretResolved map[string]string
	if len(resolved) > 0 {
		secretResolved = resolved[0]
	}

	nCan := utils.NotificationCanonical(n.Type, n.Name)
	notif, ok := pp.Notification(nCan)
	if !ok {
		w.logger.Warn("notification not found in pipeline", "notification", nCan)
		return false
	}
	nt, ok := pp.NotificationType(n.Type)
	if !ok {
		w.logger.Warn("notification type not found", "type", n.Type)
		return false
	}

	if nt.Notify == nil {
		return false
	}

	params := make(map[string]string)
	// Type-level params from the notify command
	for k, v := range nt.Notify.Params {
		params[k] = v
	}
	// Notification-level params (param_ prefix)
	accepted, notifyWarnings := validateParams(notif.GetParams(), nt.Params, "param_", w.logger, "notification_type", nt.Name)
	for k, v := range accepted {
		params[k] = v
	}
	// Step-level params (notify_ prefix)
	for k, v := range n.Params {
		params["notify_"+k] = v
	}
	// Build metadata
	for k, v := range buildMetadataParams(b, m) {
		params[k] = v
	}
	for k, v := range exportedVars {
		params[k] = v
	}
	// Message: step message > notification message > empty.
	// Resolved after build metadata and exported vars so that
	// $BUILD_NUMBER, $GET_*, etc. are expanded in the message.
	msg := n.Message
	if msg == "" {
		msg = notif.Message
	}
	if msg != "" {
		expanded := os.Expand(msg, func(key string) string {
			return params[key]
		})
		// Convert literal \n sequences (e.g. from PIKOCI_OUTPUT values) to real newlines.
		params["NOTIFY_MESSAGE"] = strings.ReplaceAll(expanded, `\n`, "\n")
	}

	ru, rc, ok := applyRunnerOverride(pp, nt.Notify, nt.Runner)
	if !ok {
		w.logger.Warn("runner not found for notification type", "runner", nt.Notify.Runner)
		return false
	}

	for k, v := range params {
		rc.Params[k] = v
	}
	if nt.Runner != nil {
		delete(rc.Params, "path")
	}

	replaceSecretPlaceholders(rc.Params, secretResolved)
	replaceSecretPlaceholdersInSlice(rc.Args, secretResolved)

	maxAttempts := ps.Attempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	// Append a "running" step and persist it
	stepIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{Type: "notify", Name: n.Name, Status: build.Started})
	w.updateBuild(ctx, m, *b)

	out := formatParamWarnings(notifyWarnings)
	var d time.Duration
	var err error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && maxAttempts > 1 {
			out += fmt.Sprintf("\n--- attempt %d/%d ---\n", attempt, maxAttempts)
		}

		prefix := out
		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = prefix + partial
			w.updateBuild(ctx, m, *b)
		}

		runCtx := ctx
		var cancel context.CancelFunc
		if ps.Timeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, ps.Timeout)
		}

		var attemptOut string
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, rc, secretVals, onPartialLog)
		out += attemptOut

		if cancel != nil {
			cancel()
		}

		if err == nil {
			break
		}

		if runCtx.Err() == context.DeadlineExceeded {
			out += fmt.Sprintf("\nstep timed out after %s", ps.Timeout)
		}
	}

	if err != nil {
		b.Steps[stepIdx] = build.Step{Type: "notify", Name: n.Name, Logs: out, Duration: d, Status: build.Failed}
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		w.logger.Error("failed to run notify step", "step", n.Name, "error", err)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, n.Name, ps.OnFailure, "on_failure", secretResolved, exportedVars)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, n.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
		return true
	}

	b.Steps[stepIdx] = build.Step{Type: "notify", Name: n.Name, Logs: out, Duration: d, Status: build.Succeeded}
	if err := w.updateBuild(ctx, m, *b); err != nil {
		return true
	}
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, n.Name, ps.OnSuccess, "on_success", secretResolved, exportedVars)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, n.Name, ps.Ensure, "ensure", secretResolved, exportedVars)
	return false
}

// runAutoNotifications fires automatic notifications based on the build status
// and the notification's on/jobs/exclude configuration.
func (w *Worker) runAutoNotifications(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, resolved map[string]string) {
	if len(pp.Notifications) == 0 {
		return
	}

	// Map build status to event name
	var event string
	switch b.Status {
	case build.Succeeded:
		event = "success"
	case build.Failed:
		event = "failure"
	case build.Cancelled:
		event = "cancel"
	default:
		return
	}

	for _, n := range pp.Notifications {
		if len(n.On) == 0 {
			continue
		}

		// Check if event matches
		matched := false
		for _, ev := range n.On {
			if ev == "all" || ev == event {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Check job scope (expand for_each group names to instance names)
		if len(n.Jobs) > 0 {
			expandedJobs := resolvePassedJobNames(n.Jobs, pp)
			found := false
			for _, jn := range expandedJobs {
				if jn == m.JobName {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if len(n.Exclude) > 0 {
			expandedExclude := resolvePassedJobNames(n.Exclude, pp)
			excluded := false
			for _, jn := range expandedExclude {
				if jn == m.JobName {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		ps := job.PlanStep{
			Type: job.StepTypeNotify,
			Notify: &job.NotifyStep{
				Type:    n.Type,
				Name:    n.Name,
				Params:  map[string]string{"build_status": event},
				Message: n.Message,
			},
		}
		w.runNotifyStep(ctx, m, b, cwd, pp, *ps.Notify, ps, nil, secretValuesFromResolved(resolved), resolved)
	}
}

// buildPullParams assembles the environment parameters needed to pull a resource version.
// Returns (nil, 0, nil) if an error occurred (error is already handled via failBuild).
// The second return value is the version ID actually used.
// The third return value contains warnings about unrecognized params.
func (w *Worker) buildPullParams(ctx context.Context, m workitem.Body, b *build.Build, rt restype.ResourceType, r resource.Resource, g job.GetStep, resolvedVersionID uint32) (map[string]string, uint32, []string) {
	var params map[string]string
	if rt.Pull != nil && rt.Pull.Params != nil {
		params = make(map[string]string)
		for k, v := range rt.Pull.Params {
			params[k] = v
		}
	} else {
		params = make(map[string]string)
	}

	dbvers, _, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, nil, nil, 0)
	if err != nil {
		w.failBuild(ctx, m, *b, fmt.Errorf("failed to list resource versions: %w", err))
		return nil, 0, nil
	}

	// Version priority: resolvedVersionID (from passed constraints) > m.VersionID (from queue) > latest
	versionID := resolvedVersionID
	if versionID == 0 {
		versionID = m.VersionID
	}

	if versionID != 0 {
		var found bool
		for _, ver := range dbvers {
			if ver.ID == versionID {
				found = true
				for k, v := range ver.Version {
					flattenVersionValue(params, "version_"+k, v)
				}
				break
			}
		}
		if !found {
			w.failBuild(ctx, m, *b, fmt.Errorf("no version found for resource %q", r.Canonical))
			return nil, 0, nil
		}
	} else {
		if len(dbvers) == 0 {
			w.failBuild(ctx, m, *b, fmt.Errorf("no versions for resource %q", r.Canonical))
			return nil, 0, nil
		}
		slices.Reverse(dbvers)
		versionID = dbvers[0].ID
		for k, v := range dbvers[0].Version {
			flattenVersionValue(params, "version_"+k, v)
		}
	}

	accepted, pullWarnings := validateParams(r.GetParams(), rt.Params, "param_", w.logger, "resource_type", rt.Name)
	for k, v := range accepted {
		params[k] = v
	}

	return params, versionID, pullWarnings
}

// runHooks runs a list of hooks (on_success, on_failure, on_cancel, ensure) and appends
// the results as build steps.
func (w *Worker) runHooks(ctx context.Context, m workitem.Body, b *build.Build, steps *[]build.Step, cwd string, pp *pipeline.Pipeline, stepName string, hooks []job.HookStep, hookType string, resolved map[string]string, exportedVars map[string]string, buildStatus ...string) {
	secretVals := secretValuesFromResolved(resolved)
	for i, h := range hooks {
		// Compute step name early so we can use it for the running step
		name := hookType
		if stepName != "" {
			name = stepName + ":" + hookType
		}
		if len(hooks) > 1 {
			if stepName != "" {
				name = fmt.Sprintf("%s:%d:%s", stepName, i, hookType)
			} else {
				name = fmt.Sprintf("%d:%s", i, hookType)
			}
		}

		var out string
		var d time.Duration

		switch h.Type {
		case job.StepTypeRunner:
			if h.Runner == nil {
				continue
			}
			ru, ok := pp.Runner(h.Runner.Runner)
			if !ok {
				continue
			}
			rc := *h.Runner
			params := make(map[string]string)
			for k, v := range rc.Params {
				params[k] = v
			}
			for k, v := range buildMetadataParams(b, m) {
				params[k] = v
			}
			rc.Params = params
			replaceSecretPlaceholders(rc.Params, resolved)
			replaceSecretPlaceholdersInSlice(rc.Args, resolved)
			if len(buildStatus) > 0 {
				rc.Params["BUILD_STATUS"] = buildStatus[0]
			}

			// Append a "running" step and persist it
			stepIdx := len(*steps)
			*steps = append(*steps, build.Step{Type: "hook", Name: name, Status: build.Started})
			w.updateBuild(ctx, m, *b)

			onPartialLog := func(partial string) {
				(*steps)[stepIdx].Logs = partial
				w.updateBuild(ctx, m, *b)
			}

			var runErr error
			out, d, runErr = w.runRunner(ctx, ru, cwd, rc, secretVals, onPartialLog)
			hookStatus := build.Succeeded
			if runErr != nil {
				hookStatus = build.Failed
			}
			(*steps)[stepIdx] = build.Step{Type: "hook", Name: name, Logs: out, Duration: d, Status: hookStatus}
			if err := w.updateBuild(ctx, m, *b); err != nil {
				return
			}
			continue
		case job.StepTypePut:
			if h.Put == nil {
				continue
			}
			ps := job.PlanStep{
				Type: job.StepTypePut,
				Put:  h.Put,
			}
			w.runPutStep(ctx, m, b, cwd, pp, *h.Put, ps, nil, secretVals, resolved)
			continue
		case job.StepTypeNotify:
			if h.Notify == nil {
				continue
			}
			ps := job.PlanStep{
				Type:   job.StepTypeNotify,
				Notify: h.Notify,
			}
			w.runNotifyStep(ctx, m, b, cwd, pp, *h.Notify, ps, exportedVars, secretVals, resolved)
			continue
		default:
			continue
		}
	}
}

// processResourceCheck handles periodic resource version checks.
func (w *Worker) processResourceCheck(ctx context.Context, m workitem.Body, cwd string, pp *pipeline.Pipeline) {
	r, ok := pp.Resource(m.ResourceCanonical)
	if !ok {
		w.logger.Error("resource not found in pipeline", "resource", m.ResourceCanonical)
		return
	}
	rt, ok := pp.ResourceType(r.Type)
	if !ok {
		w.logger.Error("resource type not found", "type", r.Type, "resource", m.ResourceCanonical)
		return
	}

	if rt.Source == "pikoci://trigger" {
		w.processResourceCheckTrigger(ctx, m, pp, r)
		return
	}

	if rt.Check == nil {
		w.logger.Error("resource type has no check command", "type", r.Type)
		return
	}
	w.logger.Info("running resource check", "resource", m.ResourceCanonical, "type", r.Type)

	params := make(map[string]string)
	for k, v := range rt.Check.Params {
		params[k] = v
	}

	dbvers, _, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, nil, nil, 0)
	if err != nil {
		w.logger.Error("failed to list resource versions", "error", err)
		return
	}
	if len(dbvers) != 0 {
		for k, v := range dbvers[0].Version {
			flattenVersionValue(params, "version_"+k, v)
		}
	}
	accepted, checkWarnings := validateParams(r.GetParams(), rt.Params, "param_", w.logger, "resource_type", rt.Name)
	for k, v := range accepted {
		params[k] = v
	}

	resolved, err := w.resolveSecretVars(ctx, cwd, pp)
	if err != nil {
		w.logger.Error("failed to resolve secret vars for resource check", "error", err)
		r.Logs = err.Error()
		if nerr := w.pikoci.UpdatePipelineResource(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, r); nerr != nil {
			w.logger.Error("failed update resource", "resource", r.Canonical, "pipeline", m.PipelineCanonical, "error", nerr)
		}
		return
	}
	ru, rc, ok := applyRunnerOverride(pp, rt.Check, rt.Runner)
	if !ok {
		return
	}

	for k, v := range params {
		rc.Params[k] = v
	}

	if resourceCacheEnabled(rt, r) {
		cd, err := w.cacheDir(m.TeamCanonical, m.PipelineCanonical, r.Canonical)
		if err != nil {
			w.logger.Error("failed to create cache dir", "error", err)
		} else {
			rc.Params["CACHE_DIR"] = cd
		}
	}

	replaceSecretPlaceholders(rc.Params, resolved)
	replaceSecretPlaceholdersInSlice(rc.Args, resolved)

	checkWarnStr := formatParamWarnings(checkWarnings)
	out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
	if err != nil {
		r.Logs = checkWarnStr + out
		if nerr := w.pikoci.UpdatePipelineResource(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, r); nerr != nil {
			w.logger.Error("failed update resource", "resource", r.Canonical, "pipeline", m.PipelineCanonical, "error", nerr)
		}
		w.logger.Error("failed to run resource check", "error", err)
		return
	}

	if r.Logs != checkWarnStr || checkWarnStr != "" {
		r.Logs = checkWarnStr
		if err := w.pikoci.UpdatePipelineResource(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, r); err != nil {
			w.logger.Error("failed update resource", "resource", r.Canonical, "pipeline", m.PipelineCanonical, "error", err)
			return
		}
	}

	sout := strings.Split(strings.Trim(out, "\n"), "\n")
	rawVers := sout[len(sout)-1]
	if rawVers == "" {
		return
	}

	vers := make([]map[string]interface{}, 0)
	if err := json.Unmarshal([]byte(rawVers), &vers); err != nil {
		w.logger.Error("failed to unmarshal versions", "raw", rawVers, "error", err)
		r.Logs = fmt.Sprintf("failed to Unmarshal versions(%s): %v", rawVers, err)
		if nerr := w.pikoci.UpdatePipelineResource(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, r); nerr != nil {
			w.logger.Error("failed update resource", "resource", r.Canonical, "pipeline", m.PipelineCanonical, "error", nerr)
		}
		return
	}

	for _, v := range vers {
		cv, err := w.pikoci.CreateResourceVersion(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, resource.Version{
			Version: v,
		})
		if err != nil {
			if isDuplicateKeyError(err) {
				w.logger.Info("duplicate version skipped",
					"pipeline", m.PipelineCanonical, "resource", r.Canonical, "version", v)
				continue
			}
			w.logger.Error("failed to create resource version", "error", err)
			return
		}
		w.logger.Info("new version created, triggering jobs",
			"pipeline", m.PipelineCanonical, "resource", r.Canonical, "version_id", cv.ID)
		w.triggerResourceJobs(ctx, m, pp, r, cv)
	}
}

func (w *Worker) processResourceCheckTrigger(ctx context.Context, m workitem.Body, pp *pipeline.Pipeline, r resource.Resource) {
	w.logger.Info("running trigger resource check", "resource", r.Canonical)

	// Get latest resource version to find the last trigger_id
	var afterID uint32
	dbvers, _, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, nil, nil, 0)
	if err != nil {
		w.logger.Error("failed to list resource versions for trigger check", "error", err)
		return
	}
	if len(dbvers) > 0 {
		if tid, ok := dbvers[0].Version["trigger_id"]; ok {
			switch v := tid.(type) {
			case float64:
				afterID = uint32(v)
			case json.Number:
				n, _ := v.Int64()
				afterID = uint32(n)
			}
		}
	}

	triggers, err := w.pikoci.ListTriggersAfter(ctx, m.TeamCanonical, r.Canonical, afterID)
	if err != nil {
		w.logger.Error("failed to list triggers", "error", err)
		return
	}

	for _, t := range triggers {
		version := make(map[string]interface{})
		for k, v := range t.Version {
			version[k] = v
		}
		version["trigger_id"] = float64(t.ID)

		cv, err := w.pikoci.CreateResourceVersion(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, resource.Version{
			Version: version,
		})
		if err != nil {
			if isDuplicateKeyError(err) {
				w.logger.Info("duplicate trigger version skipped",
					"pipeline", m.PipelineCanonical, "resource", r.Canonical, "trigger_id", t.ID)
				continue
			}
			w.logger.Error("failed to create resource version from trigger", "error", err)
			return
		}
		w.logger.Info("trigger version created, triggering jobs",
			"pipeline", m.PipelineCanonical, "resource", r.Canonical, "trigger_id", t.ID, "version_id", cv.ID)
		w.triggerResourceJobs(ctx, m, pp, r, cv)
	}
}

func (w *Worker) runPutStepTrigger(ctx context.Context, m workitem.Body, b *build.Build, pp *pipeline.Pipeline, p job.PutStep, ps job.PlanStep) bool {
	rCan := utils.ResourceCanonical(p.Type, p.Name)

	// Collect put params as version data
	version := make(map[string]interface{})
	for k, v := range p.Params {
		version[k] = v
	}
	// Add metadata
	version["trigger_pipeline"] = m.PipelineCanonical
	version["trigger_job"] = m.JobName
	version["trigger_build"] = b.BuildNumber

	_, err := w.pikoci.CreateTrigger(ctx, m.TeamCanonical, rCan, version)

	// Append step to build as Succeeded
	status := build.Succeeded
	var logs string
	if err != nil {
		status = build.Failed
		logs = fmt.Sprintf("failed to create trigger: %v", err)
		w.logger.Error("failed to create trigger", "resource", rCan, "error", err)
	} else {
		w.logger.Info("trigger created", "resource", rCan, "pipeline", m.PipelineCanonical)
	}

	b.Steps = append(b.Steps, build.Step{Type: "put", Name: p.Name, Logs: logs, Status: status})
	w.updateBuild(ctx, m, *b)

	if err != nil {
		b.Status = build.Failed
		w.failBuild(ctx, m, *b, nil)
		return true
	}

	return false
}

// triggerResourceJobs triggers jobs that depend on a resource via "get" with trigger=true.
// If the resource is pinned to a specific version, only that version triggers builds.
func (w *Worker) triggerResourceJobs(ctx context.Context, m workitem.Body, pp *pipeline.Pipeline, r resource.Resource, cv *resource.Version) {
	// Check if the resource is pinned to a different version
	if r.PinnedVersionID != nil && cv.ID != *r.PinnedVersionID {
		w.logger.Info("resource is pinned, skipping job triggers",
			"resource", r.Canonical, "pinned_version", *r.PinnedVersionID, "new_version", cv.ID)
		return
	}

	for _, j := range pp.Jobs {
		if j.Paused {
			continue
		}
		for _, ps := range j.FlatPlanSteps() {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			g := ps.Get
			// If Passed is not 0 it means is waiting for another job
			// and this trigger is only for resources
			if g.Name == r.Name && g.Type == r.Type && g.Trigger && len(g.Passed) == 0 {
				// Create a pending build. Retry up to 3 times on transient
				// errors (e.g. SQLite database-locked) to avoid permanently
				// losing trigger builds.
				var nb *build.Build
				var err error
				for attempt := range 3 {
					nb, err = w.pikoci.CreateJobBuild(ctx, m.TeamCanonical, pp.Canonical, j.Name, build.Build{
						Status:            build.Pending,
						VersionID:         cv.ID,
						ResourceCanonical: r.Canonical,
					})
					if err == nil {
						break
					}
					if attempt < 2 {
						time.Sleep(50 * time.Millisecond)
					}
				}
				if err != nil {
					w.logger.Error("failed to create pending build for trigger", "job", j.Name, "error", err)
					continue
				}
				w.logger.Info("created trigger build",
					"pipeline", pp.Canonical, "job", j.Name, "resource", r.Canonical,
					"version_id", cv.ID, "step", g.Name, "build_id", nb.ID)
			}
		}
	}
}

func (w *Worker) pollForCancellation(apiCtx, jobCtx context.Context, cancel context.CancelFunc, m workitem.Body, buildNumber string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-jobCtx.Done():
			return
		case <-ticker.C:
			b, err := w.pikoci.GetJobBuild(apiCtx, m.TeamCanonical, m.PipelineCanonical, m.JobName, buildNumber)
			if err != nil {
				w.logger.Error("cancellation poll failed", "build_number", buildNumber, "error", err)
				continue
			}
			if b.Status == build.Cancelled {
				w.logger.Info("build cancelled, stopping job", "build_number", buildNumber)
				cancel()
				return
			}
		}
	}
}

// updateBuild persists the current build state to the DB.
// It uses apiCtx (the parent server context) so that DB writes survive
// job-level cancellation but are still cancelled on server shutdown.
func (w *Worker) updateBuild(_ context.Context, m workitem.Body, b build.Build) error {
	if b.SuppressUpdates {
		if b.OnUpdate != nil {
			b.OnUpdate()
		}
		return nil
	}
	dbCtx := w.dbContext()
	err := w.updateBuildWithRetry(dbCtx, m, b)
	if err != nil {
		w.logger.Error("failed update build", "pipeline", m.PipelineCanonical, "job", m.JobName, "error", err)
	}
	return err
}

func (w *Worker) failBuild(_ context.Context, m workitem.Body, b build.Build, err error) {
	b.Status = build.Failed
	if err != nil {
		b.Error = err.Error()
		w.logger.Error(err.Error())
	}
	if b.SuppressUpdates {
		return
	}
	dbCtx := w.dbContext()
	if uerr := w.updateBuildWithRetry(dbCtx, m, b); uerr != nil {
		w.logger.Error("failed update build", "pipeline", m.PipelineCanonical, "job", m.JobName, "error", uerr)
	}
}

// updateBuildWithRetry retries the UpdateJobBuild call with exponential backoff.
// This gives separated workers the best chance to report results when the server
// is restarting.
func (w *Worker) updateBuildWithRetry(ctx context.Context, m workitem.Body, b build.Build) error {
	var err error
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	for attempt := 0; attempt <= len(delays); attempt++ {
		err = w.pikoci.UpdateJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.BuildNumber, b)
		if err == nil {
			return nil
		}
		if attempt < len(delays) {
			w.logger.Warn("retrying build update", "pipeline", m.PipelineCanonical, "job", m.JobName, "attempt", attempt+1, "error", err)
			select {
			case <-time.After(delays[attempt]):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}

// dbContext returns the appropriate context for DB operations.
// Uses apiCtx if set (normal server mode), falls back to background
// context for tests and local mode.
func (w *Worker) dbContext() context.Context {
	if w.apiCtx != nil {
		return w.apiCtx
	}
	return context.Background()
}

func (w *Worker) deleteBuild(ctx context.Context, m workitem.Body, b build.Build) {
	if err := w.pikoci.DeleteJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.BuildNumber); err != nil {
		w.logger.Error("failed delete build", "pipeline", m.PipelineCanonical, "job", m.JobName, "error", err)
	}
}

// streamWriter is a thread-safe writer that captures stdout/stderr output
// for streaming to the UI while a command is running.
type streamWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sw *streamWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.buf.Write(p)
}

func (sw *streamWriter) String() string {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.buf.String()
}

// prepareShellRunner adjusts the runner and command for the built-in "shell"
// runner. It handles defaulting the shell to /bin/sh, validates that exactly
// one of cmd/file is set, and rewrites the runner for file mode execution.
func prepareShellRunner(ru *runner.Runner, rc *utils.RunnerCommand, cwd string) error {
	_, cmdSet := rc.Params["cmd"]
	_, fileSet := rc.Params["file"]
	if cmdSet && fileSet {
		return fmt.Errorf("shell runner: cannot set both \"cmd\" and \"file\"")
	}
	if !cmdSet && !fileSet {
		return fmt.Errorf("shell runner: must set either \"cmd\" or \"file\"")
	}

	_, shellExplicit := rc.Params["shell"]

	if !shellExplicit {
		if rc.Params == nil {
			rc.Params = make(map[string]string)
		}
		rc.Params["shell"] = "/bin/sh"
	}

	if fileSet {
		file := rc.Params["file"]
		if !filepath.IsAbs(file) {
			file = filepath.Join(cwd, file)
		}
		if shellExplicit {
			ru.Run = utils.RunCommand{Path: rc.Params["shell"], Args: []string{file}}
		} else {
			if err := os.Chmod(file, 0755); err != nil {
				return fmt.Errorf("shell runner: chmod file: %w", err)
			}
			ru.Run = utils.RunCommand{Path: file, Args: nil}
		}
		// Remove file param so it doesn't leak into env vars.
		delete(rc.Params, "file")
	}

	return nil
}

func (w *Worker) runRunner(ctx context.Context, ru runner.Runner, cwd string, rc utils.RunnerCommand, secretVals []string, onPartialLog ...func(string)) (string, time.Duration, error) {
	if ru.Name == "shell" {
		if err := prepareShellRunner(&ru, &rc, cwd); err != nil {
			return err.Error(), 0, err
		}
	}

	envs := map[string]string{"WORKDIR": cwd}
	for k, v := range rc.Params {
		envs[k] = v
	}
	envFn := func(p string) string {
		if v, ok := envs[p]; ok {
			return v
		}
		return os.Getenv(p)
	}

	var args []string
	var out string
	for _, a := range ru.Run.Args {
		if a == "$args" {
			// Pass command args through without os.Expand.
			// The $param_* and $version_* variables are set as env vars
			// on the process, so the shell expands them naturally.
			// This allows shell scripts to use local variables and awk
			// without Go's os.Expand destroying them.
			args = append(args, rc.Args...)
			continue
		}
		if a == "$env" {
			// Inject "-e KEY=VALUE" flags for each env var. This is used
			// by the Docker runner to forward params (including exported
			// GET_*/TASK_* step metadata) into the container environment.
			// Runner-internal params are excluded to avoid breaking the
			// container command (e.g. multi-line cmd values).
			for k, v := range envs {
				if !isRunnerInternalParam(k) {
					args = append(args, "-e", k+"="+v)
				}
			}
			// Inject PIKOCI_OUTPUT with the path remapped from host to
			// container. Detect the container workdir by scanning for
			// "-w <path>" in the already-resolved args.
			if hostPath, ok := envs["PIKOCI_OUTPUT"]; ok {
				containerPath := hostPath
				if cw := findContainerWorkdir(args); cw != "" {
					containerPath = cw + "/" + filepath.Base(hostPath)
				}
				args = append(args, "-e", "PIKOCI_OUTPUT="+containerPath)
			}
			continue
		}
		ea := os.Expand(a, envFn)
		if ea != "" {
			args = append(args, ea)
		}
	}

	cmdPath := os.Expand(ru.Run.Path, envFn)
	if cmdPath == "" {
		// Empty command path (e.g. cron pull/push with empty block), skip execution.
		return "", 0, nil
	}

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	cmd.Dir = cwd
	cmd.Env = cmd.Environ()
	for k, v := range envs {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	w.logger.Debug("running command", "cmd", cmd.String(), "envs", createKeyValuePairs(envs))

	var partialCb func(string)
	if len(onPartialLog) > 0 {
		partialCb = onPartialLog[0]
	}

	sw := &streamWriter{}
	cmd.Stdout = sw
	cmd.Stderr = sw

	start := time.Now()
	if err := cmd.Start(); err != nil {
		out := err.Error()
		return out, time.Since(start), err
	}

	var ticker *time.Ticker
	var wg sync.WaitGroup
	done := make(chan struct{})
	if partialCb != nil {
		ticker = time.NewTicker(2 * time.Second)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ticker.C:
					if ctx.Err() != nil {
						return
					}
					partialCb(maskSecrets(sw.String(), secretVals))
				case <-ctx.Done():
					return
				case <-done:
					return
				}
			}
		}()
	}

	err := cmd.Wait()
	if ticker != nil {
		ticker.Stop()
	}
	close(done)
	wg.Wait()

	duration := time.Since(start)
	out += sw.String()
	if err != nil {
		out += "\n" + err.Error()
		// List available env var names (not values) to help diagnose typos.
		names := make([]string, 0, len(envs))
		for k := range envs {
			if !isRunnerInternalParam(k) {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		out += "\n\n--- Available environment variables ---\n"
		for _, n := range names {
			out += n + "\n"
		}
	}
	out = maskSecrets(out, secretVals)
	w.logger.Debug("finished running command", "out", out)

	return out, duration, err
}

// fetchSecrets resolves secret values for the given secrets map (secret_type name -> path)
// and returns them as a map of "secret_<key>" env vars.
func (w *Worker) fetchSecrets(ctx context.Context, cwd string, pp *pipeline.Pipeline, secrets map[string]string) (map[string]string, error) {
	result := make(map[string]string)
	for stName, path := range secrets {
		st, ok := pp.SecretType(stName)
		if !ok {
			return nil, fmt.Errorf("secret_type %q not found in pipeline", stName)
		}

		// Build params: config values + path param
		params := make(map[string]string)
		for k, v := range st.Get.Params {
			params[k] = v
		}
		// Add config values as param_<key>
		for k, v := range st.Config {
			params["param_"+k] = v
		}
		// Add path as param_path (the dynamic per-step value), only if set.
		// When empty, the secret_type's config path (from st.Config) is used as default.
		if path != "" {
			params["param_path"] = path
		}

		ru, rc, ok := applyRunnerOverride(pp, &st.Get, st.Runner)
		if !ok {
			return nil, fmt.Errorf("runner %q not found for secret_type %q", st.Get.Runner, st.Name)
		}

		// Resolve relative param_path to absolute so the command works
		// regardless of which working directory it runs in.
		if p, ok := params["param_path"]; ok && !filepath.IsAbs(p) {
			abs, err := filepath.Abs(p)
			if err == nil {
				params["param_path"] = abs
			}
		}

		for k, v := range params {
			rc.Params[k] = v
		}

		out, _, err := w.runRunner(ctx, ru, cwd, rc, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch secret from %q at %q: %s\n%w", stName, path, out, err)
		}

		// Parse output based on format config
		format := st.Config["format"]
		if format == "" {
			if p := st.Config["path"]; p != "" {
				switch strings.ToLower(filepath.Ext(p)) {
				case ".json":
					format = "json"
				case ".env":
					format = "env"
				case ".yml", ".yaml":
					format = "yaml"
				default:
					format = "raw"
				}
			}
		}
		var secretData map[string]string
		switch format {
		case "env":
			secretData = parseEnvFormat(out)
		case "raw":
			// Raw format: entire file content as a single "content" key.
			secretData = map[string]string{"content": out}
		case "json":
			if err := json.Unmarshal([]byte(out), &secretData); err != nil {
				return nil, fmt.Errorf("failed to parse secret output from %q as JSON: %w", stName, err)
			}
		case "yaml", "yml":
			if err := yaml.Unmarshal([]byte(out), &secretData); err != nil {
				return nil, fmt.Errorf("failed to parse secret output from %q as YAML: %w", stName, err)
			}
		default:
			// Default: parse last line of stdout as JSON object
			sout := strings.Split(strings.Trim(out, "\n"), "\n")
			rawJSON := sout[len(sout)-1]
			if err := json.Unmarshal([]byte(rawJSON), &secretData); err != nil {
				return nil, fmt.Errorf("failed to parse secret output from %q as JSON: %w", stName, err)
			}
		}

		for k, v := range secretData {
			result["secret_"+k] = v
		}
	}
	return result, nil
}

// startServices starts all service steps and returns the successfully started service steps.
func (w *Worker) startServices(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, serviceSteps []job.ServiceStep) []job.ServiceStep {
	var started []job.ServiceStep
	for _, ss := range serviceSteps {
		svc, ok := pp.Service(ss.Name)
		if !ok {
			w.logger.Error("service not found", "service", ss.Name)
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, fmt.Errorf("service %q not found in pipeline", ss.Name))
			return started
		}

		ru, rc, ok := applyRunnerOverride(pp, &svc.Start, svc.Runner)
		if !ok {
			w.logger.Error("runner not found for service start", "runner", svc.Start.Runner, "service", ss.Name)
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, fmt.Errorf("runner %q not found for service %q start", svc.Start.Runner, ss.Name))
			return started
		}

		params, svcWarnings := w.serviceParams(b, m, svc.Start.Params, ss.Params, svc.Params, svc.Name)
		for k, v := range params {
			rc.Params[k] = v
		}
		if svc.Runner != nil {
			delete(rc.Params, "path")
		}

		// Append a "running" step and persist it
		stepIdx := len(b.Steps)
		b.Steps = append(b.Steps, build.Step{Type: "service", Name: ss.Name + ":start", Status: build.Started})
		w.updateBuild(ctx, m, *b)

		svcWarnStr := formatParamWarnings(svcWarnings)
		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = svcWarnStr + partial
			w.updateBuild(ctx, m, *b)
		}

		out, d, err := w.runRunner(ctx, ru, cwd, rc, nil, onPartialLog)
		out = svcWarnStr + out
		if err != nil {
			b.Steps[stepIdx] = build.Step{Type: "service", Name: ss.Name + ":start", Logs: out, Duration: d, Status: build.Failed}
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, nil)
			w.logger.Error("failed to start service", "service", ss.Name, "error", err)
			return started
		}

		b.Steps[stepIdx] = build.Step{Type: "service", Name: ss.Name + ":start", Logs: out, Duration: d, Status: build.Succeeded}
		if err := w.updateBuild(ctx, m, *b); err != nil {
			return started
		}
		started = append(started, ss)
	}
	return started
}

// buildMetadataParams returns the standard build metadata environment variables.
func buildMetadataParams(b *build.Build, m workitem.Body) map[string]string {
	return map[string]string{
		"BUILD_NUMBER":        b.BuildNumber,
		"BUILD_JOB_NAME":      m.JobName,
		"BUILD_PIPELINE_NAME": m.PipelineCanonical,
		"BUILD_TEAM_NAME":     m.TeamCanonical,
	}
}

// flattenVersionValue flattens a version metadata value into params with the
// given key prefix. Nested maps are recursively flattened with "_" separators
// (e.g. prefix "version_metadata" + {"sha": "abc"} → "version_metadata_sha" = "abc").
// Scalar values are converted to strings directly.
func flattenVersionValue(params map[string]string, prefix string, v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, nested := range val {
			flattenVersionValue(params, prefix+"_"+k, nested)
		}
	case []interface{}:
		for i, item := range val {
			flattenVersionValue(params, fmt.Sprintf("%s_%d", prefix, i), item)
		}
	case string:
		params[prefix] = val
	case float64:
		if val == float64(int64(val)) {
			params[prefix] = fmt.Sprintf("%d", int64(val))
		} else {
			params[prefix] = fmt.Sprintf("%g", val)
		}
	case bool:
		params[prefix] = fmt.Sprintf("%t", val)
	case nil:
		params[prefix] = ""
	default:
		params[prefix] = fmt.Sprintf("%v", val)
	}
}

// runnerInternalParams are params used by the runner template itself (e.g. cmd,
// image, WORKDIR). These must not be injected as -e flags via $env because they
// can contain multi-line values or special characters that break container CLIs.
var runnerInternalParams = map[string]bool{
	"cmd":           true,
	"image":         true,
	"WORKDIR":       true,
	"path":          true,
	"PIKOCI_OUTPUT": true,
	"script":        true,
	"shell":         true,
	"file":          true,
}

// isRunnerInternalParam reports whether the given key is a runner-internal
// parameter that should be excluded from $env injection.
func isRunnerInternalParam(key string) bool {
	return runnerInternalParams[key]
}

// findContainerWorkdir scans resolved args for "-w <path>", "--workdir <path>",
// or "--workdir=<path>" and returns the container workdir path. Returns "" if not found.
func findContainerWorkdir(args []string) string {
	for i := 0; i < len(args); i++ {
		if (args[i] == "-w" || args[i] == "--workdir") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(args[i], "--workdir=") {
			return strings.TrimPrefix(args[i], "--workdir=")
		}
	}
	return ""
}

// validateParams filters userParams through allowedParams, returning accepted
// params (with prefix prepended to each key) and warning strings for any
// unrecognized params. For unrecognized params it uses Levenshtein distance to
// suggest the closest match if distance <= 2.
func validateParams(userParams map[string]string, allowedParams []string, prefix string, logger *slog.Logger, typeName, entityName string) (map[string]string, []string) {
	accepted := make(map[string]string)
	var warnings []string
	for k, v := range userParams {
		if slices.Contains(allowedParams, k) {
			accepted[prefix+k] = v
			continue
		}
		msg := fmt.Sprintf("WARNING: param %q is not declared by %s %q and was ignored", k, typeName, entityName)
		best := ""
		bestDist := 3
		for _, p := range allowedParams {
			d := levenshtein.ComputeDistance(k, p)
			if d < bestDist {
				bestDist = d
				best = p
			}
		}
		if best != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", best)
		}
		logger.Warn(msg)
		warnings = append(warnings, msg)
	}
	return accepted, warnings
}

// formatParamWarnings joins warning strings into a single block suitable for
// prepending to step log output.
func formatParamWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	return strings.Join(warnings, "\n") + "\n"
}

// taskRunKnownFields are the HCL field names of RunnerCommand that are parsed
// structurally. Anything else ends up in the Params map via ",remain". If a
// Params key is a close Levenshtein match to one of these, the user likely made
// a typo in their pipeline HCL.
var taskRunKnownFields = []string{"args"}

// validateTaskRunParams checks whether any keys in the user-supplied Params map
// of a task's RunnerCommand look like typos of known HCL fields (e.g. "arg"
// instead of "args"). Returns warning strings for any suspicious keys.
func validateTaskRunParams(params map[string]string, logger *slog.Logger, stepName string) []string {
	var warnings []string
	for k := range params {
		// Skip keys that are known runner-internal params — these are
		// legitimate user-supplied values consumed by the runner template.
		if isRunnerInternalParam(k) {
			continue
		}
		for _, field := range taskRunKnownFields {
			if k == field {
				continue
			}
			if levenshtein.ComputeDistance(k, field) <= 2 {
				msg := fmt.Sprintf("WARNING: %q in task %q is not a known field and was treated as a param (did you mean %q?)", k, stepName, field)
				logger.Warn(msg)
				warnings = append(warnings, msg)
			}
		}
	}
	return warnings
}

// sanitizeStepName converts a step name to a safe environment variable prefix
// by uppercasing it and replacing non-alphanumeric characters with underscores.
func sanitizeStepName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// maxOutputFileSize is the maximum size of the $PIKOCI_OUTPUT file (1MB).
const maxOutputFileSize = 1 << 20

// parseOutputFile reads a KEY=VALUE file written by a task step and returns
// the parsed key-value pairs. Keys are uppercased with non-alphanumeric chars
// replaced by underscores. Empty lines and lines starting with # are skipped.
// Returns an empty map if the file doesn't exist, is empty, or exceeds maxOutputFileSize.
func parseOutputFile(path string, logger *slog.Logger) map[string]string {
	result := make(map[string]string)

	info, err := os.Stat(path)
	if err != nil {
		return result
	}
	if info.Size() > maxOutputFileSize {
		logger.Warn("PIKOCI_OUTPUT file exceeds 1MB limit, ignoring", "path", path, "size", info.Size())
		return result
	}

	data, err := os.ReadFile(path)
	if err != nil {
		logger.Warn("failed to read PIKOCI_OUTPUT file", "path", path, "error", err)
		return result
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			logger.Warn("invalid line in PIKOCI_OUTPUT (no '=')", "line", line)
			continue
		}
		key := sanitizeStepName(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		result[key] = strings.TrimSpace(v)
	}
	return result
}

// serviceParams builds the environment parameters for a service command,
// merging the command's own params with build info and per-job overrides.
func (w *Worker) serviceParams(b *build.Build, m workitem.Body, cmdParams map[string]string, overrides map[string]string, allowedParams []string, svcName string) (map[string]string, []string) {
	params := make(map[string]string)
	for k, v := range cmdParams {
		params[k] = v
	}
	for k, v := range buildMetadataParams(b, m) {
		params[k] = v
	}
	accepted, warnings := validateParams(overrides, allowedParams, "param_", w.logger, "service_type", svcName)
	for k, v := range accepted {
		params[k] = v
	}
	return params, warnings
}

// waitForServices runs ready_check for all started services that have one.
// Returns false if any ready_check times out.
func (w *Worker) waitForServices(ctx context.Context, m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, startedServices []job.ServiceStep) bool {
	type readyResult struct {
		name string
		out  string
		d    time.Duration
		err  error
	}

	// Pre-allocate a "running" build step for each ready_check so
	// the UI shows progress while polling.
	type readyStepRef struct {
		name    string
		stepIdx int
	}
	var refs []readyStepRef
	for _, ss := range startedServices {
		svc, ok := pp.Service(ss.Name)
		if !ok || svc.ReadyCheck == nil {
			continue
		}
		rcRunnerName := svc.ReadyCheck.Runner
		if svc.Runner != nil && svc.ReadyCheck.Runner == "exec" {
			rcRunnerName = svc.Runner.Runner
		}
		if _, ok := pp.Runner(rcRunnerName); !ok {
			continue
		}
		idx := len(b.Steps)
		b.Steps = append(b.Steps, build.Step{Type: "service", Name: ss.Name + ":ready", Status: build.Started})
		refs = append(refs, readyStepRef{name: ss.Name, stepIdx: idx})
	}
	if len(refs) > 0 {
		w.updateBuild(ctx, m, *b)
	}

	// Build a map for goroutines to find their step index.
	stepIdxByName := make(map[string]int)
	for _, ref := range refs {
		stepIdxByName[ref.name] = ref.stepIdx
	}

	var wg sync.WaitGroup
	results := make(chan readyResult, len(startedServices))

	for _, ss := range startedServices {
		svc, ok := pp.Service(ss.Name)
		if !ok || svc.ReadyCheck == nil {
			continue
		}

		rcCmd := &svc.ReadyCheck.RunnerCommand
		ru, readyRC, ok := applyRunnerOverride(pp, rcCmd, svc.Runner)
		if !ok {
			continue
		}

		buildNumber := b.BuildNumber
		wg.Add(1)
		go func(svcName string, rc service.ReadyCheck, ru runner.Runner, readyRC utils.RunnerCommand, overrides map[string]string) {
			defer wg.Done()

			interval := 1 * time.Second
			if rc.Interval != "" {
				if d, err := time.ParseDuration(rc.Interval); err == nil {
					interval = d
				}
			}
			timeout := 60 * time.Second
			if rc.Timeout != "" {
				if d, err := time.ParseDuration(rc.Timeout); err == nil {
					timeout = d
				}
			}

			bm := buildMetadataParams(&build.Build{BuildNumber: buildNumber}, m)
			for k, v := range bm {
				readyRC.Params[k] = v
			}
			for k, v := range overrides {
				readyRC.Params["param_"+k] = v
			}

			runCmd := readyRC

			deadline := time.After(timeout)
			start := time.Now()
			var lastOut string
			var lastErr error
			for {
				select {
				case <-deadline:
					results <- readyResult{
						name: svcName,
						out:  lastOut + fmt.Sprintf("\nready_check timed out after %s", timeout),
						d:    time.Since(start),
						err:  fmt.Errorf("ready_check timed out after %s", timeout),
					}
					return
				case <-ctx.Done():
					results <- readyResult{
						name: svcName,
						out:  "context cancelled",
						d:    time.Since(start),
						err:  ctx.Err(),
					}
					return
				default:
				}

				lastOut, _, lastErr = w.runRunner(ctx, ru, cwd, runCmd, nil)
				if lastErr == nil {
					results <- readyResult{
						name: svcName,
						out:  lastOut,
						d:    time.Since(start),
					}
					return
				}
				time.Sleep(interval)
			}
		}(ss.Name, *svc.ReadyCheck, ru, readyRC, ss.Params)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	allReady := true
	for r := range results {
		idx, ok := stepIdxByName[r.name]
		if !ok {
			continue
		}
		if r.err != nil {
			b.Steps[idx] = build.Step{Type: "service", Name: r.name + ":ready", Logs: r.out, Duration: r.d, Status: build.Failed}
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, nil)
			w.logger.Error("service ready_check failed", "service", r.name, "error", r.err)
			allReady = false
		} else {
			b.Steps[idx] = build.Step{Type: "service", Name: r.name + ":ready", Logs: r.out, Duration: r.d, Status: build.Succeeded}
			w.updateBuild(ctx, m, *b)
		}
	}

	return allReady
}

// stopServices stops all started services unconditionally.
// Uses a fresh context to ensure cleanup runs even if the parent context is cancelled.
func (w *Worker) stopServices(m workitem.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, startedServices []job.ServiceStep) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, ss := range startedServices {
		svc, ok := pp.Service(ss.Name)
		if !ok {
			continue
		}

		ru, rc, ok := applyRunnerOverride(pp, &svc.Stop, svc.Runner)
		if !ok {
			w.logger.Error("runner not found for service stop", "runner", svc.Stop.Runner, "service", ss.Name)
			continue
		}

		params, stopWarnings := w.serviceParams(b, m, svc.Stop.Params, ss.Params, svc.Params, svc.Name)
		for k, v := range params {
			rc.Params[k] = v
		}
		if svc.Runner != nil {
			delete(rc.Params, "path")
		}

		// Append a "running" step and persist it
		stepIdx := len(b.Steps)
		b.Steps = append(b.Steps, build.Step{Type: "service", Name: ss.Name + ":stop", Status: build.Started})
		w.updateBuild(stopCtx, m, *b)

		stopWarnStr := formatParamWarnings(stopWarnings)
		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = stopWarnStr + partial
			w.updateBuild(stopCtx, m, *b)
		}

		out, d, err := w.runRunner(stopCtx, ru, cwd, rc, nil, onPartialLog)
		out = stopWarnStr + out
		stepStatus := build.Succeeded
		if err != nil {
			stepStatus = build.Failed
			w.logger.Error("failed to stop service", "service", ss.Name, "error", err)
		}
		b.Steps[stepIdx] = build.Step{Type: "service", Name: ss.Name + ":stop", Logs: out, Duration: d, Status: stepStatus}
		w.updateBuild(stopCtx, m, *b)
	}
}

// resolveSecretVars resolves all secret-backed variable placeholders by fetching
// the actual secret values from the configured secret types. Variables sharing
// the same secret type and path are batched into a single fetch call.
func (w *Worker) resolveSecretVars(ctx context.Context, cwd string, pp *pipeline.Pipeline) (map[string]string, error) {
	if len(pp.SecretVars) == 0 {
		return nil, nil
	}
	if w.LocalMode {
		return nil, nil
	}

	// Group variables by (type, path) to avoid duplicate fetches.
	type fetchKey struct{ typ, path string }
	groups := make(map[fetchKey][]string) // fetchKey -> []varName
	for varName, sv := range pp.SecretVars {
		k := fetchKey{sv.Type, sv.Path}
		groups[k] = append(groups[k], varName)
	}

	resolved := make(map[string]string)
	for k, varNames := range groups {
		secrets, err := w.fetchSecrets(ctx, cwd, pp, map[string]string{k.typ: k.path})
		if err != nil {
			return nil, fmt.Errorf("failed to resolve secrets from %q at %q: %w", k.typ, k.path, err)
		}
		for _, varName := range varNames {
			sv := pp.SecretVars[varName]
			placeholder := fmt.Sprintf("__pikoci_secret:%s:%s:%s__", sv.Type, sv.Path, sv.Key)
			val, ok := secrets["secret_"+sv.Key]
			if !ok {
				return nil, fmt.Errorf("secret for variable %q: key %q not found in response", varName, sv.Key)
			}
			resolved[placeholder] = val
		}
	}
	return resolved, nil
}

// replaceSecretPlaceholders replaces secret placeholder strings in a params map
// with the actual resolved secret values.
func replaceSecretPlaceholders(params map[string]string, resolved map[string]string) {
	for k := range params {
		for placeholder, val := range resolved {
			if strings.Contains(params[k], placeholder) {
				params[k] = strings.ReplaceAll(params[k], placeholder, val)
			}
		}
	}
}

// replaceSecretPlaceholdersInSlice replaces secret placeholder strings in a
// string slice with the actual resolved secret values.
func replaceSecretPlaceholdersInSlice(ss []string, resolved map[string]string) {
	for i := range ss {
		for placeholder, val := range resolved {
			if strings.Contains(ss[i], placeholder) {
				ss[i] = strings.ReplaceAll(ss[i], placeholder, val)
			}
		}
	}
}

// secretValuesFromResolved extracts unique non-empty secret values from a
// resolved placeholder map, sorted longest-first so longer secrets are
// masked before shorter substrings. Values shorter than 3 characters are
// skipped to avoid false positives.
func secretValuesFromResolved(resolved map[string]string) []string {
	seen := make(map[string]struct{}, len(resolved))
	var vals []string
	for _, v := range resolved {
		if v == "" || len(v) < 3 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		vals = append(vals, v)
	}
	if len(vals) == 0 {
		return nil
	}
	sort.Slice(vals, func(i, j int) bool {
		return len(vals[i]) > len(vals[j])
	})
	return vals
}

// maskSecrets replaces all secret values in s with "***".
func maskSecrets(s string, secretValues []string) string {
	for _, v := range secretValues {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// parseEnvFormat parses KEY=VALUE lines (e.g. .env files) into a map.
// Comment lines (#), blank lines, and lines without a valid variable name are ignored.
// Values optionally wrapped in single or double quotes are stripped.
func parseEnvFormat(data string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 1 {
			continue
		}
		key := line[:idx]
		// Validate key is a valid variable name
		valid := true
		for i, c := range key {
			if i == 0 {
				if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
					valid = false
					break
				}
			} else {
				if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
					valid = false
					break
				}
			}
		}
		if !valid {
			continue
		}
		val := line[idx+1:]
		// Strip surrounding quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result
}

func createKeyValuePairs(m map[string]string) string {
	b := new(bytes.Buffer)
	for key, value := range m {
		fmt.Fprintf(b, "%s=%s ", key, value)
	}
	return b.String()
}

// isDuplicateKeyError returns true if the error is a unique constraint violation.
func isDuplicateKeyError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") || // SQLite
		strings.Contains(s, "Duplicate entry") || // MySQL
		strings.Contains(s, "duplicate key value") // PostgreSQL
}

// trackJob records a cancel function for a running job (used by gRPC mode).
func (w *Worker) trackJob(buildID string, cancel context.CancelFunc) {
	w.jobCancelsMu.Lock()
	if w.jobCancels != nil {
		w.jobCancels[buildID] = cancel
	}
	w.jobCancelsMu.Unlock()
}

// untrackJob removes a job's cancel function.
func (w *Worker) untrackJob(buildID string) {
	w.jobCancelsMu.Lock()
	if w.jobCancels != nil {
		delete(w.jobCancels, buildID)
	}
	w.jobCancelsMu.Unlock()
}

// cancelJob cancels a running job by its build ID.
func (w *Worker) cancelJob(buildID string) {
	w.jobCancelsMu.Lock()
	cancel, ok := w.jobCancels[buildID]
	w.jobCancelsMu.Unlock()
	if ok {
		cancel()
	}
}

// activeJobCount returns the number of currently running jobs.
func (w *Worker) activeJobCount() int {
	w.jobCancelsMu.Lock()
	defer w.jobCancelsMu.Unlock()
	if w.jobCancels == nil {
		return 0
	}
	return len(w.jobCancels)
}
