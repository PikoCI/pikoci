package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adrg/xdg"
	"github.com/google/uuid"
	"github.com/xescugc/pikoci/pikoci"
	"github.com/xescugc/pikoci/pikoci/build"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/pipeline"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/runner"
	"github.com/xescugc/pikoci/pikoci/service"
	"github.com/xescugc/pikoci/pikoci/utils"
	"gocloud.dev/pubsub"
)

type Service interface {
	Run(ctx context.Context, q, t string) error
}

type Worker struct {
	jobTopic          queue.Topic
	pikoci            pikoci.Service
	jobSubscription   queue.Subscription
	checkSubscription queue.Subscription

	draining atomic.Bool
	logger   *slog.Logger

	// ResourceOverrides maps resource canonical names to local directory paths.
	// When set, get steps for these resources will copy the local directory
	// instead of running the resource type's pull command.
	ResourceOverrides map[string]string
	// LocalMode enables local execution behavior: skips passed-constraints,
	// version-availability checks, put steps, and secret resolution.
	LocalMode bool
}

func New(s pikoci.Service, jobTopic queue.Topic, jobSub, checkSub queue.Subscription, l *slog.Logger) *Worker {
	return &Worker{
		pikoci:            s,
		jobTopic:          jobTopic,
		jobSubscription:   jobSub,
		checkSubscription: checkSub,
		logger:            l,
	}
}

func (w *Worker) Drain() {
	w.draining.Store(true)
}

func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("Worker waiting for messages...")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	if w.jobSubscription != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.receiveLoop(ctx, w.jobSubscription, "job"); err != nil {
				errs <- err
			}
		}()
	}

	if w.checkSubscription != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.receiveLoop(ctx, w.checkSubscription, "check"); err != nil {
				errs <- err
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case err := <-errs:
		return err
	case <-done:
		return nil
	}
}

func (w *Worker) receiveLoop(ctx context.Context, sub queue.Subscription, kind string) error {
	for {
		if w.draining.Load() {
			w.logger.Info("Worker draining, stopping message receive", "queue", kind)
			return nil
		}
		msg, err := sub.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil && w.draining.Load() {
				w.logger.Info("Worker draining, stopping message receive", "queue", kind)
				return nil
			}
			return fmt.Errorf("failed to receive %s message: %w", kind, err)
		}

		w.logger.Info("received message", "queue", kind, "body", string(msg.Body))

		var m queue.Body
		if err := json.Unmarshal(msg.Body, &m); err != nil {
			w.logger.Error("failed unmarshal message body", "queue", kind, "error", err)
			msg.Ack()
			continue
		}

		// Ack immediately to prevent re-delivery while the job runs.
		// Jobs can take minutes (e.g. docker builds), which exceeds
		// the pubsub ack deadline and causes duplicate triggers.
		msg.Ack()

		cwd, err := w.createWorkDir()
		if err != nil {
			return err
		}

		w.processMessage(ctx, m, cwd)
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

func (w *Worker) processMessage(ctx context.Context, m queue.Body, cwd string) {
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
func (w *Worker) processJob(ctx context.Context, m queue.Body, cwd string, pp *pipeline.Pipeline) {
	// Resolve the oldest pending build from the DB rather than trusting the
	// message's BuildID. Queue messages are treated as wake-up signals — the
	// DB is the source of truth for ordering. This ensures strict FIFO even
	// when pub/sub backends deliver messages out of order.
	if !w.LocalMode {
		oldest, err := w.pikoci.FindOldestPendingBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName)
		if err != nil {
			w.logger.Error("failed to find oldest pending build",
				"pipeline", m.PipelineCanonical, "job", m.JobName, "error", err)
			return
		}
		if oldest == nil {
			w.logger.Info("no pending builds, skipping",
				"pipeline", m.PipelineCanonical, "job", m.JobName, "msg_build_id", m.BuildID)
			return
		}
		if oldest.ID != m.BuildID {
			w.logger.Info("reordering: oldest pending build differs from message",
				"pipeline", m.PipelineCanonical, "job", m.JobName,
				"msg_build_id", m.BuildID, "oldest_build_id", oldest.ID)
		}
		m.BuildID = oldest.ID
		if oldest.VersionID != 0 {
			m.VersionID = oldest.VersionID
		}
		if oldest.ResourceCanonical != "" {
			m.ResourceCanonical = oldest.ResourceCanonical
		}
	}

	if m.BuildID == 0 {
		w.logger.Error("missing build_id in message",
			"pipeline", m.PipelineCanonical, "job", m.JobName)
		return
	}

	w.logger.Info("processJob called",
		"pipeline", m.PipelineCanonical, "job", m.JobName, "build_id", m.BuildID,
		"version_id", m.VersionID, "resource", m.ResourceCanonical)

	nb, err := w.pikoci.StartPendingBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, m.BuildID)
	if err != nil {
		if errors.Is(err, pikoci.ErrConcurrencyLimit) {
			w.logger.Info("concurrency limit, re-queuing",
				"pipeline", m.PipelineCanonical, "job", m.JobName, "build_id", m.BuildID)
			mb, _ := json.Marshal(m)
			if err := w.jobTopic.Send(ctx, &pubsub.Message{Body: mb}); err != nil {
				w.logger.Error("failed to re-queue", "error", err)
			}
			time.Sleep(2 * time.Second)
			return
		}
		if errors.Is(err, pikoci.ErrBuildNotPending) {
			w.logger.Info("build no longer pending, skipping", "build_id", m.BuildID)
			return
		}
		w.logger.Error("failed to start pending build",
			"pipeline", m.PipelineCanonical, "job", m.JobName, "build_id", m.BuildID, "error", err)
		return
	}

	b := build.Build{
		ID:          nb.ID,
		BuildNumber: nb.BuildNumber,
		Status:      build.Started,
		StartedAt:   nb.StartedAt,
		Steps:       []build.Step{},
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

	// Poll for cancellation in background
	go w.pollForCancellation(ctx, jobCtx, jobCancel, m, b.BuildNumber)

	j, err := w.pikoci.GetPipelineJob(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName)
	if err != nil {
		w.failBuild(ctx, m, b, fmt.Errorf("failed to get job: %w", err))
		return
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
		for _, ps := range j.Plan {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			if vid, ok := stepVersions[ps.Get.Name]; ok {
				resolvedVersions[ps.Get.ResourceCanonical()] = vid
			}
		}
	} else {
		ok, rv := w.checkPassedConstraints(jobCtx, m, &b, j)
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

	failed, resolved := w.runPlan(jobCtx, m, &b, cwd, pp, j, resolvedVersions)

	// Handle user-initiated cancellation
	if jobCtx.Err() == context.Canceled {
		current, err := w.pikoci.GetJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.BuildNumber)
		if err == nil && current.Status == build.Cancelled {
			b.Status = build.Cancelled
			w.updateBuild(ctx, m, b)
			w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnCancel, "on_cancel", resolved, "cancelled")
			w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.Ensure, "ensure", resolved, "cancelled")
			return
		}
	}

	if !failed {
		b.Status = build.Succeeded
		if err := w.updateBuild(jobCtx, m, b); err != nil {
			return
		}
		w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnSuccess, "on_success", resolved, "succeeded")
	} else {
		// Ensure local b reflects the Failed status set by failBuild (which
		// operates on a copy) so that any subsequent updateBuild calls from
		// hooks don't overwrite the DB status back to Started.
		if b.Status != build.Failed {
			b.Status = build.Failed
		}
		w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.OnFailure, "on_failure", resolved, "failed")
	}
	status := "succeeded"
	if b.Status == build.Failed {
		status = "failed"
	}
	w.runHooks(ctx, m, &b, &b.Job, cwd, pp, "", j.Ensure, "ensure", resolved, status)
}

func (w *Worker) notifyNextPendingBuild(ctx context.Context, m queue.Body) {
	pending, err := w.pikoci.FindOldestPendingBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName)
	if err != nil {
		w.logger.Warn("failed to find next pending build", "error", err)
		return
	}
	if pending == nil {
		return
	}
	msg := queue.Body{
		TeamCanonical:     m.TeamCanonical,
		PipelineCanonical: m.PipelineCanonical,
		JobName:           m.JobName,
		BuildID:           pending.ID,
	}
	mb, _ := json.Marshal(msg)
	if err := w.jobTopic.Send(ctx, &pubsub.Message{Body: mb}); err != nil {
		w.logger.Warn("failed to notify next pending build", "build_id", pending.ID, "error", err)
	}
}

// checkPassedConstraints verifies that all jobs in the "passed" list have a
// successful build that used a common resource version. The returned map
// contains resourceCanonical → resolvedVersionID for each get step with Passed.
// If no common version exists, the build is deleted and (false, nil) is returned.
func (w *Worker) checkPassedConstraints(ctx context.Context, m queue.Body, b *build.Build, j *job.Job) (bool, map[string]uint32) {
	resolvedVersions := make(map[string]uint32)
	for _, ps := range j.Plan {
		if ps.Type != job.StepTypeGet || ps.Get == nil {
			continue
		}
		g := ps.Get
		if len(g.Passed) == 0 {
			continue
		}
		rCan := g.ResourceCanonical()

		var intersection map[uint32]bool
		var hasSucceeded bool
		for _, p := range g.Passed {
			builds, err := w.pikoci.ListJobBuilds(ctx, m.TeamCanonical, m.PipelineCanonical, p)
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
					if step.Type == "get" && step.Name == g.Name && step.VersionID != 0 {
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

// checkVersionAvailability verifies that all get steps in the plan have a
// version available to pull. If any get step has no version, the build is
// deleted and false is returned (same behavior as checkPassedConstraints).
// This prevents hooks from running when no work can be done.
func (w *Worker) checkVersionAvailability(ctx context.Context, m queue.Body, b *build.Build, j *job.Job, pp *pipeline.Pipeline) bool {
	for _, ps := range j.Plan {
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

		dbvers, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical)
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
func (w *Worker) runPlan(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, j *job.Job, resolvedVersions map[string]uint32) (bool, map[string]string) {
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
		return true, nil
	}

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
				return true, resolved
			}
			if !w.waitForServices(ctx, m, b, cwd, pp, startedServices) {
				return true, resolved
			}
		case job.StepTypeGet:
			if ps.Get == nil {
				continue
			}
			if w.runGetStep(ctx, m, b, cwd, pp, *ps.Get, ps, resolvedVersions, resolved) {
				return true, resolved
			}
		case job.StepTypeTask:
			if ps.Task == nil {
				continue
			}
			if w.runTaskStep(ctx, m, b, cwd, pp, *ps.Task, ps, resolved) {
				return true, resolved
			}
		case job.StepTypePut:
			if ps.Put == nil {
				continue
			}
			if w.runPutStep(ctx, m, b, cwd, pp, *ps.Put, ps, resolved) {
				return true, resolved
			}
		}
	}
	return false, resolved
}

// runGetStep runs a single get step (resource pull).
// Returns true if the step failed.
func (w *Worker) runGetStep(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, g job.GetStep, ps job.PlanStep, resolvedVersions map[string]uint32, resolved ...map[string]string) bool {
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

	params, usedVersionID := w.buildPullParams(ctx, m, b, rt, r, g, passedVersionID)
	if params == nil {
		return true
	}

	ru, ok := pp.Runner(rt.Pull.Runner)
	if !ok {
		return false
	}

	replaceSecretPlaceholders(params, secretResolved)

	for k, v := range buildMetadataParams(b, m) {
		params[k] = v
	}

	pullArgs := make([]string, len(rt.Pull.Args))
	copy(pullArgs, rt.Pull.Args)
	replaceSecretPlaceholdersInSlice(pullArgs, secretResolved)

	rc := utils.RunnerCommand{
		Runner: rt.Pull.Runner,
		Args:   pullArgs,
		Params: params,
	}

	maxAttempts := ps.Attempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	// Append a "running" step and persist it
	stepIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{Type: "get", Name: g.Name, Status: build.Started})
	w.updateBuild(ctx, m, *b)

	var out string
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
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, rc, onPartialLog)
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
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.OnFailure, "on_failure", secretResolved)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.Ensure, "ensure", secretResolved)
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
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.OnSuccess, "on_success", secretResolved)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, g.Name, ps.Ensure, "ensure", secretResolved)
	return false
}

// runGetStepLocal handles a get step by copying a local directory into the
// working directory instead of running the resource type's pull command. This
// is used for local execution with --resource overrides. A real copy is used
// instead of a symlink because symlinks break with Docker volume mounts (the
// container can't resolve host-side symlink targets).
// Returns true if the step failed.
func (w *Worker) runGetStepLocal(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, g job.GetStep, ps job.PlanStep, localPath string) bool {
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
	w.runHooks(ctx, m, b, &b.Steps, cwd, nil, g.Name, ps.OnSuccess, "on_success", nil)
	w.runHooks(ctx, m, b, &b.Steps, cwd, nil, g.Name, ps.Ensure, "ensure", nil)
	return false
}

// runTaskStep runs a single task step.
// Returns true if the step failed.
func (w *Worker) runTaskStep(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, t job.TaskStep, ps job.PlanStep, resolved ...map[string]string) bool {
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

	replaceSecretPlaceholders(t.Run.Params, secretResolved)
	replaceSecretPlaceholdersInSlice(t.Run.Args, secretResolved)

	for k, v := range buildMetadataParams(b, m) {
		t.Run.Params[k] = v
	}

	for _, input := range t.Inputs {
		if _, err := os.Stat(filepath.Join(cwd, input)); err != nil {
			errMsg := fmt.Sprintf("input %q does not exist", input)
			b.Steps = append(b.Steps, build.Step{Type: "task", Name: t.Name, Logs: errMsg, Status: build.Failed})
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, nil)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnFailure, "on_failure", secretResolved)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved)
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

	var out string
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
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, t.Run, onPartialLog)
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
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnFailure, "on_failure", secretResolved)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved)
		return true
	}

	b.Steps[stepIdx] = build.Step{Type: "task", Name: t.Name, Logs: out, Duration: d, Status: build.Succeeded}

	for _, output := range t.Outputs {
		if _, err := os.Stat(filepath.Join(cwd, output)); err != nil {
			errMsg := fmt.Sprintf("task finished but output %q was not produced", output)
			b.Steps[stepIdx] = build.Step{Type: "task", Name: t.Name, Logs: out + "\n" + errMsg, Duration: d, Status: build.Failed}
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, nil)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnFailure, "on_failure", secretResolved)
			w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved)
			return true
		}
	}

	if err := w.updateBuild(ctx, m, *b); err != nil {
		return true
	}
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.OnSuccess, "on_success", secretResolved)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, t.Name, ps.Ensure, "ensure", secretResolved)
	return false
}

// runPutStep runs a single put step (resource push).
// Returns true if the step failed.
func (w *Worker) runPutStep(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, p job.PutStep, ps job.PlanStep, resolved ...map[string]string) bool {
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
	for k, v := range r.GetParams() {
		if slices.Contains(rt.Params, k) {
			params["param_"+k] = v
		}
	}
	// Add put-step-level params with put_ prefix
	for k, v := range p.Params {
		params["put_"+k] = v
	}
	for k, v := range buildMetadataParams(b, m) {
		params[k] = v
	}

	ru, ok := pp.Runner(rt.Push.Runner)
	if !ok {
		return false
	}

	replaceSecretPlaceholders(params, secretResolved)

	pushArgs := make([]string, len(rt.Push.Args))
	copy(pushArgs, rt.Push.Args)
	replaceSecretPlaceholdersInSlice(pushArgs, secretResolved)

	rc := utils.RunnerCommand{
		Runner: rt.Push.Runner,
		Args:   pushArgs,
		Params: params,
	}

	maxAttempts := ps.Attempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	// Append a "running" step and persist it
	stepIdx := len(b.Steps)
	b.Steps = append(b.Steps, build.Step{Type: "put", Name: p.Name, Status: build.Started})
	w.updateBuild(ctx, m, *b)

	var out string
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
		attemptOut, d, err = w.runRunner(runCtx, ru, cwd, rc, onPartialLog)
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
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.OnFailure, "on_failure", secretResolved)
		w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.Ensure, "ensure", secretResolved)
		return true
	}

	b.Steps[stepIdx] = build.Step{Type: "put", Name: p.Name, Logs: out, Duration: d, Status: build.Succeeded}
	if err := w.updateBuild(ctx, m, *b); err != nil {
		return true
	}
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.OnSuccess, "on_success", secretResolved)
	w.runHooks(ctx, m, b, &b.Steps, cwd, pp, p.Name, ps.Ensure, "ensure", secretResolved)
	return false
}

// buildPullParams assembles the environment parameters needed to pull a resource version.
// Returns (nil, 0) if an error occurred (error is already handled via failBuild).
// The second return value is the version ID actually used.
func (w *Worker) buildPullParams(ctx context.Context, m queue.Body, b *build.Build, rt restype.ResourceType, r resource.Resource, g job.GetStep, resolvedVersionID uint32) (map[string]string, uint32) {
	var params map[string]string
	if rt.Pull != nil && rt.Pull.Params != nil {
		params = make(map[string]string)
		for k, v := range rt.Pull.Params {
			params[k] = v
		}
	} else {
		params = make(map[string]string)
	}

	dbvers, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical)
	if err != nil {
		w.failBuild(ctx, m, *b, fmt.Errorf("failed to list resource versions: %w", err))
		return nil, 0
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
					params["version_"+k] = fmt.Sprintf("%s", v)
				}
				break
			}
		}
		if !found {
			w.failBuild(ctx, m, *b, fmt.Errorf("no version found for resource %q", r.Canonical))
			return nil, 0
		}
	} else {
		if len(dbvers) == 0 {
			w.failBuild(ctx, m, *b, fmt.Errorf("no versions for resource %q", r.Canonical))
			return nil, 0
		}
		slices.Reverse(dbvers)
		versionID = dbvers[0].ID
		for k, v := range dbvers[0].Version {
			params["version_"+k] = fmt.Sprintf("%s", v)
		}
	}

	for k, v := range r.GetParams() {
		if slices.Contains(rt.Params, k) {
			params["param_"+k] = v
		}
	}

	return params, versionID
}

// runHooks runs a list of hooks (on_success, on_failure, on_cancel, ensure) and appends
// the results as build steps.
func (w *Worker) runHooks(ctx context.Context, m queue.Body, b *build.Build, steps *[]build.Step, cwd string, pp *pipeline.Pipeline, stepName string, hooks []job.HookStep, hookType string, resolved map[string]string, buildStatus ...string) {
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

			out, d, _ = w.runRunner(ctx, ru, cwd, rc, onPartialLog)

			(*steps)[stepIdx] = build.Step{Type: "hook", Name: name, Logs: out, Duration: d, Status: build.Succeeded}
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
			w.runPutStep(ctx, m, b, cwd, pp, *h.Put, ps, resolved)
			continue
		default:
			continue
		}
	}
}

// processResourceCheck handles periodic resource version checks.
func (w *Worker) processResourceCheck(ctx context.Context, m queue.Body, cwd string, pp *pipeline.Pipeline) {
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

	dbvers, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical)
	if err != nil {
		w.logger.Error("failed to list resource versions", "error", err)
		return
	}
	if len(dbvers) != 0 {
		for k, v := range dbvers[0].Version {
			params["version_"+k] = fmt.Sprintf("%s", v)
		}
	}
	for k, v := range r.GetParams() {
		if slices.Contains(rt.Params, k) {
			params["param_"+k] = v
		}
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
	replaceSecretPlaceholders(params, resolved)

	ru, ok := pp.Runner(rt.Check.Runner)
	if !ok {
		return
	}

	checkArgs := make([]string, len(rt.Check.Args))
	copy(checkArgs, rt.Check.Args)
	replaceSecretPlaceholdersInSlice(checkArgs, resolved)

	rc := utils.RunnerCommand{
		Runner: rt.Check.Runner,
		Args:   checkArgs,
		Params: params,
	}
	out, _, err := w.runRunner(ctx, ru, cwd, rc)
	if err != nil {
		r.Logs = out
		if nerr := w.pikoci.UpdatePipelineResource(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical, r); nerr != nil {
			w.logger.Error("failed update resource", "resource", r.Canonical, "pipeline", m.PipelineCanonical, "error", nerr)
		}
		w.logger.Error("failed to run resource check", "error", err)
		return
	}

	if r.Logs != "" {
		r.Logs = ""
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

func (w *Worker) processResourceCheckTrigger(ctx context.Context, m queue.Body, pp *pipeline.Pipeline, r resource.Resource) {
	w.logger.Info("running trigger resource check", "resource", r.Canonical)

	// Get latest resource version to find the last trigger_id
	var afterID uint32
	dbvers, err := w.pikoci.ListResourceVersions(ctx, m.TeamCanonical, m.PipelineCanonical, r.Canonical)
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

func (w *Worker) runPutStepTrigger(ctx context.Context, m queue.Body, b *build.Build, pp *pipeline.Pipeline, p job.PutStep, ps job.PlanStep) bool {
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
func (w *Worker) triggerResourceJobs(ctx context.Context, m queue.Body, pp *pipeline.Pipeline, r resource.Resource, cv *resource.Version) {
	for _, j := range pp.Jobs {
		for _, ps := range j.Plan {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			g := ps.Get
			// If Passed is not 0 it means is waiting for another job
			// and this trigger is only for resources
			if g.Name == r.Name && g.Type == r.Type && g.Trigger && len(g.Passed) == 0 {
				// Create a pending build first
				nb, err := w.pikoci.CreateJobBuild(ctx, m.TeamCanonical, pp.Canonical, j.Name, build.Build{
					Status:            build.Pending,
					VersionID:         cv.ID,
					ResourceCanonical: r.Canonical,
				})
				if err != nil {
					w.logger.Error("failed to create pending build for trigger", "job", j.Name, "error", err)
					continue
				}
				qb := queue.Body{
					TeamCanonical:     m.TeamCanonical,
					PipelineCanonical: pp.Canonical,
					JobName:           j.Name,
					BuildID:           nb.ID,
					ResourceCanonical: r.Canonical,
					VersionID:         cv.ID,
				}
				mb, err := json.Marshal(qb)
				if err != nil {
					w.logger.Error("failed to marshal trigger body", "error", err)
					continue
				}
				w.logger.Info("sending trigger message",
					"pipeline", pp.Canonical, "job", j.Name, "resource", r.Canonical,
					"version_id", cv.ID, "step", g.Name, "build_id", nb.ID)
				if err := w.jobTopic.Send(ctx, &pubsub.Message{Body: mb}); err != nil {
					w.logger.Error("failed to send trigger message", "job", j.Name, "error", err)
				}
			}
		}
	}
}

func (w *Worker) pollForCancellation(apiCtx, jobCtx context.Context, cancel context.CancelFunc, m queue.Body, buildNumber string) {
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
func (w *Worker) updateBuild(ctx context.Context, m queue.Body, b build.Build) error {
	err := w.pikoci.UpdateJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.BuildNumber, b)
	if err != nil {
		w.logger.Error("failed update build", "pipeline", m.PipelineCanonical, "job", m.JobName, "error", err)
	}
	return err
}

func (w *Worker) failBuild(ctx context.Context, m queue.Body, b build.Build, err error) {
	b.Status = build.Failed
	if err != nil {
		b.Error = err.Error()
		w.logger.Error(err.Error())
	}
	if uerr := w.pikoci.UpdateJobBuild(ctx, m.TeamCanonical, m.PipelineCanonical, m.JobName, b.BuildNumber, b); uerr != nil {
		w.logger.Error("failed update build", "pipeline", m.PipelineCanonical, "job", m.JobName, "error", uerr)
	}
}

func (w *Worker) deleteBuild(ctx context.Context, m queue.Body, b build.Build) {
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

func (w *Worker) runRunner(ctx context.Context, ru runner.Runner, cwd string, rc utils.RunnerCommand, onPartialLog ...func(string)) (string, time.Duration, error) {
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
					partialCb(sw.String())
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
	}
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

		ru, ok := pp.Runner(st.Get.Runner)
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

		rc := utils.RunnerCommand{
			Runner: st.Get.Runner,
			Args:   st.Get.Args,
			Params: params,
		}

		out, _, err := w.runRunner(ctx, ru, cwd, rc)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch secret from %q at %q: %s\n%w", stName, path, out, err)
		}

		// Parse output based on format config
		format := st.Config["format"]
		var secretData map[string]string
		switch format {
		case "env":
			secretData = parseEnvFormat(out)
		case "raw":
			// Raw format: entire file content as a single "content" key.
			secretData = map[string]string{"content": out}
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
func (w *Worker) startServices(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, serviceSteps []job.ServiceStep) []job.ServiceStep {
	var started []job.ServiceStep
	for _, ss := range serviceSteps {
		svc, ok := pp.Service(ss.Name)
		if !ok {
			w.logger.Error("service not found", "service", ss.Name)
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, fmt.Errorf("service %q not found in pipeline", ss.Name))
			return started
		}

		ru, ok := pp.Runner(svc.Start.Runner)
		if !ok {
			w.logger.Error("runner not found for service start", "runner", svc.Start.Runner, "service", ss.Name)
			b.Status = build.Failed
			w.failBuild(ctx, m, *b, fmt.Errorf("runner %q not found for service %q start", svc.Start.Runner, ss.Name))
			return started
		}

		params := w.serviceParams(b, m, svc.Start.Params, ss.Params)

		rc := utils.RunnerCommand{
			Runner: svc.Start.Runner,
			Args:   svc.Start.Args,
			Params: params,
		}

		// Append a "running" step and persist it
		stepIdx := len(b.Steps)
		b.Steps = append(b.Steps, build.Step{Type: "service", Name: ss.Name + ":start", Status: build.Started})
		w.updateBuild(ctx, m, *b)

		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = partial
			w.updateBuild(ctx, m, *b)
		}

		out, d, err := w.runRunner(ctx, ru, cwd, rc, onPartialLog)
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
func buildMetadataParams(b *build.Build, m queue.Body) map[string]string {
	return map[string]string{
		"BUILD_NUMBER":        b.BuildNumber,
		"BUILD_JOB_NAME":      m.JobName,
		"BUILD_PIPELINE_NAME": m.PipelineCanonical,
		"BUILD_TEAM_NAME":     m.TeamCanonical,
	}
}

// serviceParams builds the environment parameters for a service command,
// merging the command's own params with build info and per-job overrides.
func (w *Worker) serviceParams(b *build.Build, m queue.Body, cmdParams map[string]string, overrides map[string]string) map[string]string {
	params := make(map[string]string)
	for k, v := range cmdParams {
		params[k] = v
	}
	for k, v := range buildMetadataParams(b, m) {
		params[k] = v
	}
	for k, v := range overrides {
		params["param_"+k] = v
	}
	return params
}

// waitForServices runs ready_check for all started services that have one.
// Returns false if any ready_check times out.
func (w *Worker) waitForServices(ctx context.Context, m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, startedServices []job.ServiceStep) bool {
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
		if _, ok := pp.Runner(svc.ReadyCheck.Runner); !ok {
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

		ru, ok := pp.Runner(svc.ReadyCheck.Runner)
		if !ok {
			continue
		}

		buildNumber := b.BuildNumber
		wg.Add(1)
		go func(svcName string, rc service.ReadyCheck, ru runner.Runner, overrides map[string]string) {
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

			params := make(map[string]string)
			for k, v := range rc.Params {
				params[k] = v
			}
			bm := buildMetadataParams(&build.Build{BuildNumber: buildNumber}, m)
			for k, v := range bm {
				params[k] = v
			}
			for k, v := range overrides {
				params["param_"+k] = v
			}

			runCmd := utils.RunnerCommand{
				Runner: rc.Runner,
				Args:   rc.Args,
				Params: params,
			}

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

				lastOut, _, lastErr = w.runRunner(ctx, ru, cwd, runCmd)
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
		}(ss.Name, *svc.ReadyCheck, ru, ss.Params)
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
func (w *Worker) stopServices(m queue.Body, b *build.Build, cwd string, pp *pipeline.Pipeline, startedServices []job.ServiceStep) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, ss := range startedServices {
		svc, ok := pp.Service(ss.Name)
		if !ok {
			continue
		}

		ru, ok := pp.Runner(svc.Stop.Runner)
		if !ok {
			w.logger.Error("runner not found for service stop", "runner", svc.Stop.Runner, "service", ss.Name)
			continue
		}

		params := w.serviceParams(b, m, svc.Stop.Params, ss.Params)

		rc := utils.RunnerCommand{
			Runner: svc.Stop.Runner,
			Args:   svc.Stop.Args,
			Params: params,
		}

		// Append a "running" step and persist it
		stepIdx := len(b.Steps)
		b.Steps = append(b.Steps, build.Step{Type: "service", Name: ss.Name + ":stop", Status: build.Started})
		w.updateBuild(stopCtx, m, *b)

		onPartialLog := func(partial string) {
			b.Steps[stepIdx].Logs = partial
			w.updateBuild(stopCtx, m, *b)
		}

		out, d, err := w.runRunner(stopCtx, ru, cwd, rc, onPartialLog)
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
