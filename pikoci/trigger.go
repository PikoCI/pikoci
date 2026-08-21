package pikoci

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/utils"
)

// FireTriggerNotifications fires on_trigger hook notifications for all jobs
// reachable (directly or transitively) from the given resource canonical.
// It is called by remote workers via the HTTP client; the server loads the
// pipeline and delegates to fireOnTriggerHooks.
func (q *PikoCI) FireTriggerNotifications(ctx context.Context, tc, pc, triggeringRCan string, versionMeta map[string]interface{}) {
	pp, err := q.Pipelines.Find(ctx, tc, pc)
	if err != nil {
		q.logger.Warn("on_trigger: failed to find pipeline", "team", tc, "pipeline", pc, "error", err)
		return
	}
	q.fireOnTriggerHooks(ctx, pp, tc, pc, triggeringRCan, versionMeta)
}

// fireOnTriggerHooks is the internal implementation of on_trigger notification
// dispatch. It accepts an already-loaded pipeline (which may have Raw bytes),
// re-parses the raw HCL to recover OnTrigger hooks (not stored in DB), and
// runs all hooks for reachable jobs in parallel goroutines.
func (q *PikoCI) fireOnTriggerHooks(ctx context.Context, pp *pipeline.Pipeline, tc, pc, triggeringRCan string, versionMeta map[string]interface{}) {
	if pp.Raw == nil {
		return
	}
	parsed, err := ReadPipeline(ctx, pp.Raw, nil)
	if err != nil {
		q.logger.Warn("on_trigger: failed to parse pipeline HCL", "team", tc, "pipeline", pc, "error", err)
		return
	}

	reachable := reachableJobs(parsed, triggeringRCan)

	var wg sync.WaitGroup
	for _, j := range reachable {
		if len(j.OnTrigger) == 0 {
			continue
		}
		jCopy := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, hook := range jCopy.OnTrigger {
				if hook.Type != job.StepTypeNotify || hook.Notify == nil {
					continue
				}
				if err := runTriggerNotifyHook(ctx, q.logger, parsed, tc, pc, jCopy.Name, *hook.Notify, versionMeta); err != nil {
					q.logger.Warn("on_trigger hook failed",
						"job", jCopy.Name,
						"notification", hook.Notify.NotificationCanonical(),
						"error", err)
				}
			}
		}()
	}
	wg.Wait()
}

// reachableJobs returns all jobs reachable when the given resource canonical
// triggers a pipeline. It starts from direct-trigger jobs (get step matching
// rCan with no passed constraints) and follows passed dependencies via BFS.
func reachableJobs(p *pipeline.Pipeline, rCan string) []job.Job {
	// Step 1: find direct-trigger jobs.
	directNames := make(map[string]bool)
	for _, j := range p.Jobs {
		for _, ps := range j.FlatPlanSteps() {
			if ps.Type != job.StepTypeGet || ps.Get == nil {
				continue
			}
			g := ps.Get
			if g.ResourceCanonical() == rCan && len(g.Passed) == 0 && g.Trigger {
				directNames[j.Name] = true
				break
			}
		}
	}

	// Step 2: BFS over passed constraints to find transitively reachable jobs.
	reachableNames := make(map[string]bool, len(directNames))
	for n := range directNames {
		reachableNames[n] = true
	}

	queue := make([]string, 0, len(directNames))
	for n := range directNames {
		queue = append(queue, n)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, j := range p.Jobs {
			if reachableNames[j.Name] {
				continue
			}
			// Check if j depends on 'current' in any get step's passed list.
			for _, ps := range j.FlatPlanSteps() {
				if ps.Type != job.StepTypeGet || ps.Get == nil {
					continue
				}
				g := ps.Get
				if len(g.Passed) == 0 {
					continue
				}
				expanded := resolvePassedJobNames(g.Passed, p.Jobs)
				for _, passed := range expanded {
					if passed == current {
						reachableNames[j.Name] = true
						queue = append(queue, j.Name)
						break
					}
				}
				if reachableNames[j.Name] {
					break
				}
			}
		}
	}

	// Return jobs in pipeline declaration order.
	var result []job.Job
	for _, j := range p.Jobs {
		if reachableNames[j.Name] {
			result = append(result, j)
		}
	}
	return result
}

// runTriggerNotifyHook runs a single notify hook step from an on_trigger block.
// It executes the notification type's command in a temporary directory with
// appropriate environment variables. Returns an error if execution fails.
func runTriggerNotifyHook(ctx context.Context, logger *slog.Logger, pp *pipeline.Pipeline, tc, pc, jn string, n job.NotifyStep, versionMeta map[string]interface{}) error {
	nCan := utils.NotificationCanonical(n.Type, n.Name)
	notif, ok := pp.Notification(nCan)
	if !ok {
		return fmt.Errorf("notification %q not found in pipeline", nCan)
	}
	nt, ok := pp.NotificationType(n.Type)
	if !ok {
		return fmt.Errorf("notification type %q not found", n.Type)
	}
	if nt.Notify == nil {
		return nil
	}

	params := buildTriggerParams(nt, notif.GetParams(), n, tc, pc, jn, versionMeta)

	tmpDir, err := os.MkdirTemp("", "pikoci-trigger-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	logger.Debug("running on_trigger hook", "job", jn, "notification", nCan)
	return execNotificationCommand(ctx, nt.Notify, tmpDir, params)
}

// buildTriggerParams constructs the environment variable map for an on_trigger
// notification execution. It follows the same layering as runNotifyStep in the
// worker: type-level params → notification params (param_ prefix) → step params
// (notify_ prefix) → build metadata → version metadata (version_ prefix).
func buildTriggerParams(nt notiftype.NotificationType, notifParams map[string]string, n job.NotifyStep, tc, pc, jn string, versionMeta map[string]interface{}) map[string]string {
	params := make(map[string]string)

	// Type-level params from the notify command definition.
	for k, v := range nt.Notify.Params {
		params[k] = v
	}
	// Notification-level params with param_ prefix.
	for k, v := range notifParams {
		params["param_"+k] = v
	}
	// Step-level params with notify_ prefix.
	for k, v := range n.Params {
		params["notify_"+k] = v
	}
	// Build-like metadata (no build number yet — this fires before build creation).
	params["BUILD_PIPELINE_NAME"] = pc
	params["BUILD_JOB_NAME"] = jn
	params["BUILD_TEAM_NAME"] = tc
	params["BUILD_NUMBER"] = ""
	// Version metadata with version_ prefix (e.g. version_ref for git SHA).
	for k, v := range versionMeta {
		params["version_"+fmt.Sprint(k)] = fmt.Sprint(v)
	}

	return params
}

// execNotificationCommand runs a notification type's exec command in workDir
// with the given params as environment variables. Supports "exec" runner
// convention: path param holds the executable, rc.Args holds the arguments.
func execNotificationCommand(ctx context.Context, rc *utils.RunnerCommand, workDir string, params map[string]string) error {
	envs := make(map[string]string, len(params)+1)
	envs["WORKDIR"] = workDir
	for k, v := range params {
		envs[k] = v
	}

	envFn := func(p string) string {
		if v, ok := envs[p]; ok {
			return v
		}
		return os.Getenv(p)
	}

	// Resolve executable path. For the "exec" runner, path comes from the
	// "path" param (set in the RunnerCommand params map by the notify block).
	path := rc.Params["path"]
	if path == "" {
		// Fallback: use runner name directly (e.g. "shell").
		path = rc.Runner
	}
	path = os.Expand(path, envFn)
	if path == "" {
		return fmt.Errorf("notification exec: empty command path for runner %q", rc.Runner)
	}

	var args []string
	for _, a := range rc.Args {
		args = append(args, os.Expand(a, envFn))
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	for k, v := range envs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec failed: %w\noutput: %s", err, string(out))
	}
	return nil
}
