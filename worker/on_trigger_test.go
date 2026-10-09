package worker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/trigger"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// These tests cover #707: on_trigger notify hooks run on the worker, where
// secret-backed params can be resolved, before the builds they announce are
// created.

const testPEM = "-----BEGIN TEST KEY-----\nMIIEpAIBAAKCAQEAon7rigger\n-----END TEST KEY-----"

// newOnTriggerWorker returns a worker on a strict mock (no AnyTimes defaults)
// whose logs are captured in the returned buffer. The buffer is at Info, the
// production default: runRunner's Debug lines print command envs unmasked
// for every step type, which is a separate issue.
func newOnTriggerWorker(t *testing.T) (*Worker, *mock.Service, *bytes.Buffer) {
	t.Helper()
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	var logs bytes.Buffer
	w := &Worker{pikoci: svc, logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	return w, svc, &logs
}

// readTestPipeline parses raw the way the server does on CreatePipeline and
// keeps Raw, as the pipeline the worker gets from GetPipeline does.
func readTestPipeline(t *testing.T, raw string) *pipeline.Pipeline {
	t.Helper()
	pp, err := pikoci.ReadPipeline(context.Background(), []byte(raw), nil)
	require.NoError(t, err)
	pp.Canonical = "my-pipeline"
	pp.Raw = []byte(raw)
	return pp
}

func writePEM(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "github_app.pem")
	require.NoError(t, os.WriteFile(p, []byte(testPEM+"\n"), 0600))
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err, "expected %s to have been written", p)
	return string(b)
}

// recorderHCL is a notification type whose hook writes its private_key param
// to keyOut. It is the shape of github-check, minus the GitHub API call.
func recorderHCL(keyOut string) string {
	return fmt.Sprintf(`
notification_type "recorder" {
  params = ["private_key"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "printf '%%s' \"$param_private_key\" > %s"]
  }
}

notification "recorder" "ci" {
  params {
    private_key = var.github_pem
  }
}
`, keyOut)
}

// pemSecretHCL is the #707 setup from deploy/pipeline.hcl: a pikoci://file
// secret type pointing at a .pem, read raw through key "content".
func pemSecretHCL(pemPath string) string {
	return fmt.Sprintf(`
secret_type "pem" {
  source = "pikoci://file"
  path   = %q
}

variable "github_pem" {
  type = string
  secret "pem" {
    key = "content"
  }
}
`, pemPath)
}

const cronJobWithHook = `
resource "cron" "timer" {}

job "build" {
  on_trigger {
    notify "recorder" "ci" {
      status = "queued"
    }
  }
  get "cron" "timer" {
    trigger = true
  }
}
`

var testBody = workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "cron.timer"}

func TestRunOnTriggerHooks_ResolvesFileSecretPem(t *testing.T) {
	dir := t.TempDir()
	keyOut := filepath.Join(dir, "key.out")
	pp := readTestPipeline(t, pemSecretHCL(writePEM(t, dir))+recorderHCL(keyOut)+cronJobWithHook)

	w, _, _ := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	got := readFile(t, keyOut)
	assert.NotContains(t, got, "__pikoci_secret", "hook received an unresolved secret placeholder")
	assert.Equal(t, testPEM, strings.TrimSpace(got))
}

func TestRunOnTriggerHooks_ResolvesExecSecret(t *testing.T) {
	dir := t.TempDir()
	keyOut := filepath.Join(dir, "key.out")
	raw := `
secret_type "vault" {
  params = ["path"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '{\"key\":\"s3cr3t-from-vault\"}'"]
  }
}

variable "github_pem" {
  type = string
  secret "vault" {
    path = "secret/data/github"
    key  = "key"
  }
}
` + recorderHCL(keyOut) + cronJobWithHook
	pp := readTestPipeline(t, raw)

	w, _, _ := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	assert.Equal(t, "s3cr3t-from-vault", readFile(t, keyOut))
}

func TestRunOnTriggerHooks_ParamLayering(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.out")
	raw := fmt.Sprintf(`
resource "cron" "timer" {}

notification_type "recorder" {
  params = ["channel"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "printf '%%s|%%s|%%s|%%s|%%s|%%s|%%s|%%s' \"$param_channel\" \"$notify_status\" \"$version_ref\" \"$BUILD_JOB_NAME\" \"$BUILD_TEAM_NAME\" \"$BUILD_PIPELINE_NAME\" \"$${BUILD_NUMBER-unset}\" \"$NOTIFY_MESSAGE\" > %s"]
  }
}

notification "recorder" "ci" {
  message = "queued $BUILD_JOB_NAME at $version_ref"
  params {
    channel = "#ci"
  }
}

job "build" {
  on_trigger {
    notify "recorder" "ci" {
      status = "queued"
    }
  }
  get "cron" "timer" {
    trigger = true
  }
}
`, out)
	pp := readTestPipeline(t, raw)

	w, _, _ := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", map[string]interface{}{"ref": "abc123"})

	// BUILD_NUMBER is set but empty: there is no build yet, and scripts use
	// `test -z "$BUILD_NUMBER"` to detect the on_trigger context.
	assert.Equal(t, "#ci|queued|abc123|build|main|my-pipeline||queued build at abc123", readFile(t, out))
}

func TestRunOnTriggerHooks_DownstreamPassedJob(t *testing.T) {
	dir := t.TempDir()
	raw := fmt.Sprintf(`
resource "cron" "timer" {}
resource "cron" "other" {}

notification_type "recorder" {
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "touch %s/$BUILD_JOB_NAME"]
  }
}

notification "recorder" "ci" {}

job "backend" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}

job "frontend" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}

job "integration" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" {
    trigger = true
    passed  = ["backend", "frontend"]
  }
}

job "unrelated" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "other" { trigger = true }
}
`, dir)
	pp := readTestPipeline(t, raw)

	w, _, _ := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	for _, j := range []string{"backend", "frontend", "integration"} {
		assert.FileExists(t, filepath.Join(dir, j), "on_trigger must fire for reachable job %q", j)
	}
	assert.NoFileExists(t, filepath.Join(dir, "unrelated"), "on_trigger must not fire for a job on another resource")
}

func TestRunOnTriggerHooks_FailureLogsAreMasked(t *testing.T) {
	dir := t.TempDir()
	raw := pemSecretHCL(writePEM(t, dir)) + `
notification_type "recorder" {
  params = ["private_key"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo \"signing with $param_private_key\"; exit 1"]
  }
}

notification "recorder" "ci" {
  params {
    private_key = var.github_pem
  }
}
` + cronJobWithHook
	pp := readTestPipeline(t, raw)

	w, _, logs := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	assert.Contains(t, logs.String(), "on_trigger hook failed", "a failing hook must be logged so it can be debugged")
	assert.Contains(t, logs.String(), "signing with ***", "the hook output must be in the log, masked")
	assert.NotContains(t, logs.String(), "MIIEpAIBAAKCAQEAon7rigger", "the secret must never reach the worker log")
}

func TestRunOnTriggerHooks_NoHooks_DoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	fetched := filepath.Join(dir, "fetched")
	raw := fmt.Sprintf(`
secret_type "spy" {
  params = ["path"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "touch %s; echo '{\"k\":\"v\"}'"]
  }
}

variable "token" {
  type = string
  secret "spy" {
    path = "x"
    key  = "k"
  }
}

resource "cron" "timer" {}

job "build" {
  get "cron" "timer" { trigger = true }
  task "use" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo ${var.token}"]
    }
  }
}
`, fetched)
	pp := readTestPipeline(t, raw)

	w, _, _ := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	assert.NoFileExists(t, fetched, "secrets must not be fetched when there is no on_trigger hook to run")
}

func TestRunOnTriggerHooks_SecretResolutionError_SkipsHooks(t *testing.T) {
	dir := t.TempDir()
	keyOut := filepath.Join(dir, "key.out")
	raw := `
secret_type "broken" {
  params = ["path"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo boom >&2; exit 1"]
  }
}

variable "github_pem" {
  type = string
  secret "broken" {
    path = "x"
    key  = "k"
  }
}
` + recorderHCL(keyOut) + cronJobWithHook
	pp := readTestPipeline(t, raw)

	w, _, logs := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	assert.NoFileExists(t, keyOut, "hooks must not run with unresolved secrets")
	assert.Contains(t, logs.String(), "failed to resolve secrets")
}

func TestTriggerResourceJobs_HooksRunBeforeBuildCreation(t *testing.T) {
	dir := t.TempDir()
	keyOut := filepath.Join(dir, "key.out")
	pp := readTestPipeline(t, pemSecretHCL(writePEM(t, dir))+recorderHCL(keyOut)+cronJobWithHook)
	r, _ := pp.Resource("cron.timer")

	w, svc, _ := newOnTriggerWorker(t)
	svc.EXPECT().CreateJobBuild(gomock.Any(), "main", "my-pipeline", "build", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, b build.Build) (*build.Build, error) {
			// "queued" must reach GitHub before the build can be picked up
			// and report "in_progress".
			assert.FileExists(t, keyOut, "the on_trigger hook must have run before the build is created")
			assert.Equal(t, uint32(10), b.VersionID)
			return &build.Build{ID: 1, BuildNumber: "1"}, nil
		}).Times(1)

	w.triggerResourceJobs(context.Background(), testBody, pp, r, &resource.Version{ID: 10, Version: map[string]interface{}{"ref": "abc"}})
}

func TestTriggerResourceJobs_HookFailureStillCreatesBuilds(t *testing.T) {
	raw := `
resource "cron" "timer" {}

notification_type "recorder" {
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "exit 1"]
  }
}

notification "recorder" "ci" {}

job "build" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}
`
	pp := readTestPipeline(t, raw)
	r, _ := pp.Resource("cron.timer")

	w, svc, _ := newOnTriggerWorker(t)
	svc.EXPECT().CreateJobBuild(gomock.Any(), "main", "my-pipeline", "build", gomock.Any()).
		Return(&build.Build{ID: 1, BuildNumber: "1"}, nil).Times(1)

	w.triggerResourceJobs(context.Background(), testBody, pp, r, &resource.Version{ID: 10})
}

func TestProcessResourceCheckTrigger_FiresHooksWithSecrets(t *testing.T) {
	dir := t.TempDir()
	keyOut := filepath.Join(dir, "key.out")
	raw := pemSecretHCL(writePEM(t, dir)) + recorderHCL(keyOut) + `
resource "trigger" "deploy" {}

job "build" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "trigger" "deploy" { trigger = true }
}
`
	pp := readTestPipeline(t, raw)
	r, _ := pp.Resource("trigger.deploy")
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "trigger.deploy"}

	w, svc, _ := newOnTriggerWorker(t)
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "trigger.deploy", nil, nil, uint32(0)).Return(nil, false, nil)
	svc.EXPECT().ListTriggersAfter(gomock.Any(), "main", "trigger.deploy", uint32(0)).Return([]*trigger.Trigger{{ID: 3, Version: map[string]interface{}{"sha": "abc"}}}, nil)
	svc.EXPECT().CreateResourceVersion(gomock.Any(), "main", "my-pipeline", "trigger.deploy", gomock.Any()).
		Return(&resource.Version{ID: 20, Version: map[string]interface{}{"sha": "abc", "trigger_id": float64(3)}}, nil)
	svc.EXPECT().CreateJobBuild(gomock.Any(), "main", "my-pipeline", "build", gomock.Any()).
		Return(&build.Build{ID: 1, BuildNumber: "1"}, nil)

	w.processResourceCheckTrigger(context.Background(), m, pp, r)

	assert.Equal(t, testPEM, strings.TrimSpace(readFile(t, keyOut)))
}

// expectCheckAfterRetrigger allows the regular check that follows a
// re-trigger: it lists the versions (limit 0) to pass the latest one to the
// check command.
func expectCheckAfterRetrigger(svc *mock.Service, rCan string) {
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", rCan, nil, nil, uint32(0)).Return(nil, false, nil)
}

// retriggerHCL has one job of each kind a manual re-trigger must handle.
func retriggerHCL(t *testing.T, dir string) string {
	return pemSecretHCL(writePEM(t, dir)) + recorderHCL(filepath.Join(dir, "key.out")) + fmt.Sprintf(`
resource_type "tick" {
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "touch %s/checked; echo '[]'"]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "true"]
  }
  push "exec" {}
}

resource "tick" "repo" {}

job "direct" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "tick" "repo" { trigger = true }
}

job "no-trigger" {
  get "tick" "repo" { trigger = false }
}

job "two-gets" {
  get "tick" "repo" { trigger = true }
  get "tick" "repo" {
    trigger = true
    passed  = ["direct"]
  }
}

job "downstream" {
  get "tick" "repo" {
    trigger = true
    passed  = ["direct"]
  }
}

job "paused" {
  get "tick" "repo" { trigger = true }
}
`, dir)
}

func TestProcessResourceCheck_Retrigger(t *testing.T) {
	dir := t.TempDir()
	pp := readTestPipeline(t, retriggerHCL(t, dir))
	for i := range pp.Jobs {
		if pp.Jobs[i].Name == "paused" {
			pp.Jobs[i].Paused = true
		}
	}
	// A pin on another version does not stop a manual re-trigger.
	pinned := uint32(99)
	for i := range pp.Resources {
		pp.Resources[i].PinnedVersionID = &pinned
	}
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "tick.repo", VersionID: 5}

	w, svc, _ := newOnTriggerWorker(t)
	expectCheckAfterRetrigger(svc, "tick.repo")
	before := uint32(6)
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "tick.repo", &before, nil, uint32(1)).
		Return([]*resource.Version{{ID: 5, Version: map[string]interface{}{"ref": "old"}}}, false, nil)

	var created []string
	svc.EXPECT().CreateJobBuild(gomock.Any(), "main", "my-pipeline", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, jn string, b build.Build) (*build.Build, error) {
			assert.FileExists(t, filepath.Join(dir, "key.out"), "hooks must run before builds are created")
			assert.NoFileExists(t, filepath.Join(dir, "checked"), "the check runs after the re-trigger, not before")
			assert.Equal(t, uint32(5), b.VersionID, "builds use the re-triggered version")
			assert.Equal(t, build.Pending, b.Status)
			created = append(created, jn)
			return &build.Build{ID: 1, BuildNumber: "1"}, nil
		}).AnyTimes()

	w.processResourceCheck(context.Background(), m, dir, pp)

	assert.Equal(t, []string{"direct", "no-trigger", "two-gets"}, created,
		"one build per job getting the resource without passed, trigger or not; none for paused or downstream jobs")
	assert.Equal(t, testPEM, strings.TrimSpace(readFile(t, filepath.Join(dir, "key.out"))))
	assert.FileExists(t, filepath.Join(dir, "checked"),
		"the claim consumed the resource's check (maybe one a webhook asked for), so it must still run")
}

func TestProcessResourceCheck_Retrigger_UnknownVersion(t *testing.T) {
	dir := t.TempDir()
	pp := readTestPipeline(t, retriggerHCL(t, dir))
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "tick.repo", VersionID: 5}

	w, svc, logs := newOnTriggerWorker(t)
	expectCheckAfterRetrigger(svc, "tick.repo")
	// Version 5 is gone (or belongs to another resource): the nearest one
	// below it comes back instead. No CreateJobBuild expectation: the strict
	// mock fails the test if a build is created.
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "tick.repo", gomock.Any(), nil, uint32(1)).
		Return([]*resource.Version{{ID: 4}}, false, nil)

	w.processResourceCheck(context.Background(), m, dir, pp)

	assert.Contains(t, logs.String(), "re-trigger: version not found")
	assert.NoFileExists(t, filepath.Join(dir, "key.out"), "no hooks for a version that does not exist")
	assert.FileExists(t, filepath.Join(dir, "checked"), "the check still runs")
}

func TestRunOnTriggerHooks_InvalidHCL_SkipsHooks(t *testing.T) {
	// The stored pipeline parsed once, but its Raw no longer does (e.g. a
	// builtin it relied on changed): log and skip instead of panicking.
	pp := &pipeline.Pipeline{Canonical: "my-pipeline", Raw: []byte(`job "x" {`)}

	w, _, logs := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	assert.Contains(t, logs.String(), "on_trigger: failed to parse pipeline HCL")
}

func TestProcessResourceCheck_Retrigger_ListVersionsError(t *testing.T) {
	dir := t.TempDir()
	pp := readTestPipeline(t, retriggerHCL(t, dir))
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "tick.repo", VersionID: 5}

	w, svc, logs := newOnTriggerWorker(t)
	expectCheckAfterRetrigger(svc, "tick.repo")
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "tick.repo", gomock.Any(), nil, uint32(1)).
		Return(nil, false, fmt.Errorf("server unavailable"))

	w.processResourceCheck(context.Background(), m, dir, pp)

	assert.Contains(t, logs.String(), "re-trigger: failed to list resource versions")
	assert.NoFileExists(t, filepath.Join(dir, "key.out"), "no hooks without the version")
	assert.FileExists(t, filepath.Join(dir, "checked"), "the check still runs when the re-trigger fails")
}

func TestProcessResourceCheck_Retrigger_CreateBuildRetries(t *testing.T) {
	raw := `
resource_type "quiet" {
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '[]'"]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "true"]
  }
  push "exec" {}
}

resource "quiet" "timer" {}

job "flaky" {
  task "first" {
    run "exec" {
      path = "/bin/sh"
      args = ["-ec", "true"]
    }
  }
  get "quiet" "timer" { trigger = true }
}

job "broken" {
  get "quiet" "timer" { trigger = true }
}

job "after" {
  get "quiet" "timer" { trigger = true }
}
`
	pp := readTestPipeline(t, raw)
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "quiet.timer", VersionID: 5}

	w, svc, logs := newOnTriggerWorker(t)
	expectCheckAfterRetrigger(svc, "quiet.timer")
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "quiet.timer", gomock.Any(), nil, uint32(1)).
		Return([]*resource.Version{{ID: 5}}, false, nil)

	attempts := map[string]int{}
	svc.EXPECT().CreateJobBuild(gomock.Any(), "main", "my-pipeline", gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, jn string, _ build.Build) (*build.Build, error) {
			attempts[jn]++
			switch {
			case jn == "flaky" && attempts[jn] < 3:
				return nil, fmt.Errorf("database is locked")
			case jn == "broken":
				return nil, fmt.Errorf("database is gone")
			}
			return &build.Build{ID: 1, BuildNumber: "1"}, nil
		}).AnyTimes()

	w.processResourceCheck(context.Background(), m, t.TempDir(), pp)

	assert.Equal(t, map[string]int{"flaky": 3, "broken": 3, "after": 1}, attempts,
		"transient errors are retried 3 times, and one failing job does not stop the others")
	assert.Contains(t, logs.String(), "re-trigger: failed to create pending build")
	assert.Contains(t, logs.String(), "job=broken")
}

func TestProcessResourceCheck_Retrigger_TriggerTypeResource(t *testing.T) {
	// A pikoci://trigger resource is re-triggered like any other, and its
	// trigger-type check still runs afterwards.
	dir := t.TempDir()
	keyOut := filepath.Join(dir, "key.out")
	raw := pemSecretHCL(writePEM(t, dir)) + recorderHCL(keyOut) + `
resource "trigger" "deploy" {}

job "build" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "trigger" "deploy" { trigger = true }
}
`
	pp := readTestPipeline(t, raw)
	m := workitem.Body{TeamCanonical: "main", PipelineCanonical: "my-pipeline", ResourceCanonical: "trigger.deploy", VersionID: 7}

	w, svc, _ := newOnTriggerWorker(t)
	before := uint32(8)
	retrigger := svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "trigger.deploy", &before, nil, uint32(1)).
		Return([]*resource.Version{{ID: 7, Version: map[string]interface{}{"trigger_id": float64(2)}}}, false, nil)
	build := svc.EXPECT().CreateJobBuild(gomock.Any(), "main", "my-pipeline", "build", gomock.Any()).
		Return(&build.Build{ID: 1, BuildNumber: "1"}, nil).After(retrigger)
	// Then the trigger-type check runs as usual and finds nothing new.
	svc.EXPECT().ListResourceVersions(gomock.Any(), "main", "my-pipeline", "trigger.deploy", nil, nil, uint32(0)).
		Return([]*resource.Version{{ID: 7, Version: map[string]interface{}{"trigger_id": float64(2)}}}, false, nil).After(build)
	svc.EXPECT().ListTriggersAfter(gomock.Any(), "main", "trigger.deploy", uint32(2)).Return(nil, nil)

	w.processResourceCheck(context.Background(), m, dir, pp)

	assert.Equal(t, testPEM, strings.TrimSpace(readFile(t, keyOut)))
}

func TestRunOnTriggerHooks_SkipsPausedJobs(t *testing.T) {
	dir := t.TempDir()
	raw := fmt.Sprintf(`
resource "cron" "timer" {}

notification_type "recorder" {
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "touch %s/$BUILD_JOB_NAME"]
  }
}

notification "recorder" "ci" {}

job "active" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}

job "paused" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}
`, dir)
	pp := readTestPipeline(t, raw)
	// Paused lives in the DB, not the HCL, as on the pipeline from GetPipeline.
	for i := range pp.Jobs {
		if pp.Jobs[i].Name == "paused" {
			pp.Jobs[i].Paused = true
		}
	}

	w, _, _ := newOnTriggerWorker(t)
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)

	assert.FileExists(t, filepath.Join(dir, "active"))
	assert.NoFileExists(t, filepath.Join(dir, "paused"), "a paused job gets no build, so it must not be announced as queued")
}

func TestRunOnTriggerHooks_VersionMetaFlattenedLikeChecks(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	raw := fmt.Sprintf(`
resource "cron" "timer" {}

notification_type "recorder" {
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "printf '%%s|%%s|%%s' \"$version_trigger_id\" \"$version_meta_sha\" \"$version_ok\" > %s"]
  }
}

notification "recorder" "ci" {}
`, out) + `
job "build" {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}
`
	pp := readTestPipeline(t, raw)

	w, _, _ := newOnTriggerWorker(t)
	// JSON numbers arrive as float64; fmt.Sprint would give "1e+06".
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", map[string]interface{}{
		"trigger_id": float64(1000000),
		"meta":       map[string]interface{}{"sha": "abc"},
		"ok":         true,
	})

	assert.Equal(t, "1000000|abc|true", readFile(t, out))
}

func TestRunOnTriggerHooks_RunInParallelWithOwnWorkdir(t *testing.T) {
	dir := t.TempDir()
	// Each hook writes a key file into $WORKDIR the way github-check does,
	// waits, then records what it reads back. Shared dirs would mix them up.
	raw := fmt.Sprintf(`
resource "cron" "timer" {}

notification_type "recorder" {
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "printf '%%s' \"$BUILD_JOB_NAME\" > \"$WORKDIR/key\"; sleep 1; printf '%%s' \"$(cat \"$WORKDIR/key\")\" > %s/$BUILD_JOB_NAME"]
  }
}

notification "recorder" "ci" {}
`, dir)
	jobs := []string{"a", "b", "c", "d"}
	for _, j := range jobs {
		raw += fmt.Sprintf(`
job %q {
  on_trigger {
    notify "recorder" "ci" {}
  }
  get "cron" "timer" { trigger = true }
}
`, j)
	}
	pp := readTestPipeline(t, raw)

	w, _, _ := newOnTriggerWorker(t)
	start := time.Now()
	w.runOnTriggerHooks(context.Background(), testBody, pp, "cron.timer", nil)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 3*time.Second, "4 hooks of 1s each must run in parallel, took %s", elapsed)
	for _, j := range jobs {
		assert.Equal(t, j, readFile(t, filepath.Join(dir, j)), "hook %q must read back its own $WORKDIR file", j)
	}
}
