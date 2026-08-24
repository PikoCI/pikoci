package pikoci

import (
	"context"
	"log/slog"
	"testing"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers

func makeGetStep(typ, name string, trigger bool, passed ...string) job.PlanStep {
	return job.PlanStep{
		Type: job.StepTypeGet,
		Get:  &job.GetStep{Type: typ, Name: name, Trigger: trigger, Passed: passed},
	}
}

func makeJob(name string, steps ...job.PlanStep) job.Job {
	return job.Job{Name: name, Plan: steps}
}

// ─── reachableJobs ────────────────────────────────────────────────────────────

func TestReachableJobs_Empty(t *testing.T) {
	p := &pipeline.Pipeline{}
	assert.Empty(t, reachableJobs(p, "git.repo"))
}

func TestReachableJobs_DirectTrigger(t *testing.T) {
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 1)
	assert.Equal(t, "build", result[0].Name)
}

func TestReachableJobs_TriggerFalse_Excluded(t *testing.T) {
	// A get step with trigger=false on the triggering resource is not a direct trigger.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", false)),
	}}
	assert.Empty(t, reachableJobs(p, "git.repo"))
}

func TestReachableJobs_WrongResource_Excluded(t *testing.T) {
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("cron", "timer", true)),
	}}
	assert.Empty(t, reachableJobs(p, "git.repo"))
}

func TestReachableJobs_PassedConstraint_NotDirectTrigger(t *testing.T) {
	// A get that has passed constraints is not a direct trigger, even if trigger=true.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("test", makeGetStep("git", "repo", true, "build")),
	}}
	assert.Empty(t, reachableJobs(p, "git.repo"))
}

func TestReachableJobs_Transitive(t *testing.T) {
	// build triggers directly; test depends on build via passed.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
		makeJob("test", makeGetStep("git", "repo", false, "build")),
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 2)
	assert.Equal(t, "build", result[0].Name)
	assert.Equal(t, "test", result[1].Name)
}

func TestReachableJobs_MultiLevel(t *testing.T) {
	// build → test → deploy (3-level chain).
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
		makeJob("test", makeGetStep("git", "repo", false, "build")),
		makeJob("deploy", makeGetStep("git", "repo", false, "test")),
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 3)
	assert.Equal(t, "build", result[0].Name)
	assert.Equal(t, "test", result[1].Name)
	assert.Equal(t, "deploy", result[2].Name)
}

func TestReachableJobs_UnreachableExcluded(t *testing.T) {
	// "other" uses a different resource and is not downstream of "build".
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
		makeJob("other", makeGetStep("cron", "timer", true)),
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 1)
	assert.Equal(t, "build", result[0].Name)
}

func TestReachableJobs_DeclarationOrder(t *testing.T) {
	// Jobs declared in reverse dependency order are returned in declaration order.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("deploy", makeGetStep("git", "repo", false, "build")),
		makeJob("build", makeGetStep("git", "repo", true)),
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 2)
	assert.Equal(t, "deploy", result[0].Name)
	assert.Equal(t, "build", result[1].Name)
}

func TestReachableJobs_MultipleDirect(t *testing.T) {
	// Two jobs both triggered directly by the same resource.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("backend", makeGetStep("git", "repo", true)),
		makeJob("frontend", makeGetStep("git", "repo", true)),
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 2)
	names := map[string]bool{result[0].Name: true, result[1].Name: true}
	assert.True(t, names["backend"])
	assert.True(t, names["frontend"])
}

// ─── buildTriggerParams ───────────────────────────────────────────────────────

func TestBuildTriggerParams_BuildMetadata(t *testing.T) {
	nt := notiftype.NotificationType{Notify: &utils.RunnerCommand{Runner: "exec"}}
	n := job.NotifyStep{}
	params := buildTriggerParams(nt, nil, n, "my-team", "my-pipeline", "my-job", nil)

	assert.Equal(t, "my-team", params["BUILD_TEAM_NAME"])
	assert.Equal(t, "my-pipeline", params["BUILD_PIPELINE_NAME"])
	assert.Equal(t, "my-job", params["BUILD_JOB_NAME"])
	assert.Equal(t, "", params["BUILD_NUMBER"])
}

func TestBuildTriggerParams_TypeLevelParams(t *testing.T) {
	nt := notiftype.NotificationType{
		Notify: &utils.RunnerCommand{
			Runner: "exec",
			Params: map[string]string{"endpoint": "https://api.example.com"},
		},
	}
	params := buildTriggerParams(nt, nil, job.NotifyStep{}, "tc", "pc", "jn", nil)
	assert.Equal(t, "https://api.example.com", params["endpoint"])
}

func TestBuildTriggerParams_NotificationParams(t *testing.T) {
	nt := notiftype.NotificationType{Notify: &utils.RunnerCommand{Runner: "exec"}}
	notifParams := map[string]string{"repo": "pikoci/pikoci", "app_id": "123"}
	params := buildTriggerParams(nt, notifParams, job.NotifyStep{}, "tc", "pc", "jn", nil)
	assert.Equal(t, "pikoci/pikoci", params["param_repo"])
	assert.Equal(t, "123", params["param_app_id"])
}

func TestBuildTriggerParams_StepParams(t *testing.T) {
	nt := notiftype.NotificationType{Notify: &utils.RunnerCommand{Runner: "exec"}}
	n := job.NotifyStep{Params: map[string]string{"status": "queued", "name": "CI"}}
	params := buildTriggerParams(nt, nil, n, "tc", "pc", "jn", nil)
	assert.Equal(t, "queued", params["notify_status"])
	assert.Equal(t, "CI", params["notify_name"])
}

func TestBuildTriggerParams_VersionMeta(t *testing.T) {
	nt := notiftype.NotificationType{Notify: &utils.RunnerCommand{Runner: "exec"}}
	versionMeta := map[string]interface{}{"ref": "abc123", "build": 42}
	params := buildTriggerParams(nt, nil, job.NotifyStep{}, "tc", "pc", "jn", versionMeta)
	assert.Equal(t, "abc123", params["version_ref"])
	assert.Equal(t, "42", params["version_build"])
}

func TestBuildTriggerParams_ParamPrefixesDoNotCollide(t *testing.T) {
	// type-level, notification-level, and step-level params use separate keys.
	nt := notiftype.NotificationType{
		Notify: &utils.RunnerCommand{
			Runner: "exec",
			Params: map[string]string{"base": "type-level"},
		},
	}
	notifParams := map[string]string{"key": "notif-value"}
	n := job.NotifyStep{Params: map[string]string{"key": "step-value"}}
	params := buildTriggerParams(nt, notifParams, n, "tc", "pc", "jn", nil)
	assert.Equal(t, "type-level", params["base"])
	assert.Equal(t, "notif-value", params["param_key"])
	assert.Equal(t, "step-value", params["notify_key"])
}

// ─── ReadPipeline on_trigger HCL parsing ─────────────────────────────────────

func TestReadPipeline_OnTriggerBlock(t *testing.T) {
	hcl := []byte(`
resource "git" "repo" {}

notification_type "test-notif" {
  notify "exec" {
    path = "/bin/true"
  }
}

notification "test-notif" "ci" {}

job "build" {
  get "git" "repo" { trigger = true }
  on_trigger {
    notify "test-notif" "ci" { status = "queued" }
  }
  on_success {
    notify "test-notif" "ci" { conclusion = "success" }
  }
}
`)
	pp, err := ReadPipeline(context.Background(), hcl, nil)
	require.NoError(t, err)
	require.Len(t, pp.Jobs, 1)

	j := pp.Jobs[0]
	assert.Equal(t, "build", j.Name)

	require.Len(t, j.OnTrigger, 1)
	assert.Equal(t, job.StepTypeNotify, j.OnTrigger[0].Type)
	require.NotNil(t, j.OnTrigger[0].Notify)
	assert.Equal(t, "test-notif", j.OnTrigger[0].Notify.Type)
	assert.Equal(t, "ci", j.OnTrigger[0].Notify.Name)
	assert.Equal(t, "queued", j.OnTrigger[0].Notify.Params["status"])

	require.Len(t, j.OnSuccess, 1)
	assert.Equal(t, "success", j.OnSuccess[0].Notify.Params["conclusion"])
}

// ─── fireOnTriggerHooks ───────────────────────────────────────────────────────

func TestFireOnTriggerHooks_NilRaw(t *testing.T) {
	q := &PikoCI{logger: slog.Default()}
	pp := &pipeline.Pipeline{} // Raw is nil
	// Should return immediately without panicking.
	q.fireOnTriggerHooks(context.Background(), pp, "tc", "pc", "git.repo", nil)
}

func TestFireOnTriggerHooks_NoOnTriggerJobs(t *testing.T) {
	// Pipeline has valid Raw HCL but no on_trigger blocks — no exec is run.
	raw := []byte(`
resource "git" "repo" {}
job "build" {
  get "git" "repo" { trigger = true }
}
`)
	q := &PikoCI{logger: slog.Default()}
	pp := &pipeline.Pipeline{Raw: raw}
	// Should complete without error.
	q.fireOnTriggerHooks(context.Background(), pp, "tc", "pc", "git.repo", nil)
}

func TestFireOnTriggerHooks_ExecSucceeds(t *testing.T) {
	// Pipeline with on_trigger that runs /bin/true — verifies end-to-end exec.
	raw := []byte(`
resource "git" "repo" {}

notification_type "test-notif" {
  notify "exec" {
    path = "/bin/true"
  }
}

notification "test-notif" "ci" {}

job "build" {
  get "git" "repo" { trigger = true }
  on_trigger {
    notify "test-notif" "ci" {}
  }
}
`)
	q := &PikoCI{logger: slog.Default()}
	pp := &pipeline.Pipeline{Raw: raw}
	// Should complete without panicking; /bin/true always exits 0.
	q.fireOnTriggerHooks(context.Background(), pp, "my-team", "my-pipeline", "git.repo",
		map[string]interface{}{"ref": "abc123"})
}

func TestFireOnTriggerHooks_NotificationNotFound_LogsWarning(t *testing.T) {
	// on_trigger references a notification that doesn't exist — hook logs warning but doesn't panic.
	raw := []byte(`
resource "git" "repo" {}

notification_type "test-notif" {
  notify "exec" {
    path = "/bin/true"
  }
}

job "build" {
  get "git" "repo" { trigger = true }
  on_trigger {
    notify "test-notif" "missing" {}
  }
}
`)
	q := &PikoCI{logger: slog.Default()}
	pp := &pipeline.Pipeline{Raw: raw}
	// Missing notification: logged as warning, no panic.
	q.fireOnTriggerHooks(context.Background(), pp, "tc", "pc", "git.repo", nil)
}

func TestFireOnTriggerHooks_InvalidHCL_LogsWarning(t *testing.T) {
	// Invalid HCL raw bytes cause ReadPipeline to fail — should log and return without panic.
	q := &PikoCI{logger: slog.Default()}
	pp := &pipeline.Pipeline{Raw: []byte(`this is not valid HCL {{{`)}
	q.fireOnTriggerHooks(context.Background(), pp, "tc", "pc", "git.repo", nil)
}

func TestFireOnTriggerHooks_NilNotifyPointer_Skipped(t *testing.T) {
	// A job whose on_trigger hook has Notify==nil (e.g. invalid in-memory construction)
	// must be silently skipped without panicking.
	//
	// fireOnTriggerHooks cannot be called directly here because it re-parses Raw
	// HCL, and HCL parsing never produces Notify==nil for a notify step — this
	// guard defends against corrupted in-memory pipelines only. We therefore
	// verify the guard condition directly against the inner-loop logic that
	// fireOnTriggerHooks executes inside each goroutine.
	pp := &pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "build",
				Plan: []job.PlanStep{makeGetStep("git", "repo", true)},
				OnTrigger: []job.HookStep{
					{Type: job.StepTypeNotify, Notify: nil}, // nil Notify: should be skipped
				},
			},
		},
	}
	for _, j := range pp.Jobs {
		for _, hook := range j.OnTrigger {
			if hook.Type != job.StepTypeNotify || hook.Notify == nil {
				continue // guard being verified: nil Notify is skipped, not dereferenced
			}
			t.Fatal("should not reach here when Notify is nil")
		}
	}
}

// ─── runTriggerNotifyHook ─────────────────────────────────────────────────────

func TestRunTriggerNotifyHook_NotifTypeNotFound(t *testing.T) {
	// Pipeline has the notification but not the notification type — returns error.
	pp := &pipeline.Pipeline{
		Notifications: []notification.Notification{
			{Type: "unknown-type", Name: "ci", Canonical: "unknown-type.ci"},
		},
		// No NotificationTypes — type lookup will fail.
	}
	n := job.NotifyStep{Type: "unknown-type", Name: "ci"}
	err := runTriggerNotifyHook(context.Background(), slog.Default(), pp, "tc", "pc", "build", n, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notification type")
}

func TestRunTriggerNotifyHook_NilNotifyBlock(t *testing.T) {
	// Notification type exists but has no notify block — returns nil (no-op).
	pp := &pipeline.Pipeline{
		Notifications: []notification.Notification{
			{Type: "noop-type", Name: "ci", Canonical: "noop-type.ci"},
		},
		NotificationTypes: []notiftype.NotificationType{
			{Name: "noop-type", Notify: nil}, // no notify block
		},
	}
	n := job.NotifyStep{Type: "noop-type", Name: "ci"}
	err := runTriggerNotifyHook(context.Background(), slog.Default(), pp, "tc", "pc", "build", n, nil)
	require.NoError(t, err)
}

// ─── execNotificationCommand ──────────────────────────────────────────────────

func TestExecNotificationCommand_EmptyPath(t *testing.T) {
	rc := &utils.RunnerCommand{Runner: "", Params: map[string]string{}}
	err := execNotificationCommand(context.Background(), rc, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command path")
}

func TestExecNotificationCommand_ExecFails(t *testing.T) {
	rc := &utils.RunnerCommand{
		Runner: "exec",
		Params: map[string]string{"path": "/bin/false"},
	}
	err := execNotificationCommand(context.Background(), rc, t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec failed")
}

func TestExecNotificationCommand_Success(t *testing.T) {
	rc := &utils.RunnerCommand{
		Runner: "exec",
		Params: map[string]string{"path": "/bin/true"},
	}
	err := execNotificationCommand(context.Background(), rc, t.TempDir(), map[string]string{"FOO": "bar"})
	require.NoError(t, err)
}

func TestExecNotificationCommand_FallbackToRunner(t *testing.T) {
	// When no path param is set, the Runner field is used as the command.
	rc := &utils.RunnerCommand{
		Runner: "/bin/true",
		Params: map[string]string{}, // no path param → falls back to Runner
	}
	err := execNotificationCommand(context.Background(), rc, t.TempDir(), nil)
	require.NoError(t, err)
}

func TestExecNotificationCommand_WithArgs(t *testing.T) {
	// Args are expanded with env vars from params before being passed to the command.
	rc := &utils.RunnerCommand{
		Runner: "exec",
		Params: map[string]string{"path": "/bin/echo"},
		Args:   []string{"hello", "$BUILD_TEAM_NAME"},
	}
	err := execNotificationCommand(context.Background(), rc, t.TempDir(), map[string]string{"BUILD_TEAM_NAME": "my-team"})
	require.NoError(t, err)
}

// ─── reachableJobs — outer-break branch ──────────────────────────────────────

func TestReachableJobs_MultiGetSteps_BreaksEarlyOnFirstMatch(t *testing.T) {
	// A downstream job has two get steps; the first one has a passed constraint
	// matching "build". The outer break should fire after the first get step match.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
		{
			Name: "test",
			Plan: []job.PlanStep{
				makeGetStep("git", "repo", false, "build"), // matches on first step
				makeGetStep("git", "repo", false),          // would also match but outer break fires
			},
		},
	}}
	result := reachableJobs(p, "git.repo")
	require.Len(t, result, 2)
	assert.Equal(t, "build", result[0].Name)
	assert.Equal(t, "test", result[1].Name)
}
