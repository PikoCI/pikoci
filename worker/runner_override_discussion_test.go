package worker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/builtin"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/pikoci/pikoci/pikoci/workitem"

	"go.uber.org/mock/gomock"
)

// These tests verify the runner-override workaround proposed in
// https://github.com/PikoCI/pikoci/discussions/698: a pipeline can redefine
// the built-in "exec" runner_type to neutralize or ban it, since
// pipeline.Pipeline.Runner looks up pipeline-defined runners before falling
// back to builtin.Runners().

// TestRunnerOverride_BannedExecRunner_IgnoresRequestedCommand confirms that
// once a pipeline redefines "exec", applyRunnerOverride resolves to the
// pipeline's definition for every step that asks for "exec" — the step's own
// requested path/args never run; only the override's command does.
func TestRunnerOverride_BannedExecRunner_IgnoresRequestedCommand(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, _ := newTestWorker(ctrl)

	ctx := context.Background()
	cwd := t.TempDir()

	// The pipeline-defined override: no matter what a step asks "exec" to
	// run, this always exits 2 instead.
	banned := runner.Runner{
		Name: "exec",
		Run:  utils.RunCommand{Path: "/bin/sh", Args: []string{"-ec", "exit 2"}},
	}

	// A step that would trivially succeed and print "hello" if the real
	// "exec" runner (Path: "$path", Args: ["$args"]) were used.
	rc := utils.RunnerCommand{
		Runner: "exec",
		Args:   []string{"hello"},
		Params: map[string]string{"path": "echo"},
	}

	out, _, err := w.runRunner(ctx, banned, cwd, rc, nil)

	require.Error(t, err, "the banned exec runner must fail instead of silently succeeding")
	assert.NotContains(t, out, "hello", "the real 'echo hello' command must never actually run")
}

// TestRunnerOverride_BannedExecRunner_BreaksBuiltinResourceCheck confirms the
// side effect flagged in the discussion: because every built-in resource type
// (git, cron, fs, artifact) defaults its check/get/put commands to the "exec"
// runner, banning "exec" pipeline-wide also breaks those built-in checks —
// not just custom task/notify steps.
func TestRunnerOverride_BannedExecRunner_BreaksBuiltinResourceCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	w, svc := newTestWorker(ctrl)

	ctx := context.Background()
	m := workitem.Body{
		TeamCanonical:     "main",
		PipelineCanonical: "test-pipeline",
		ResourceCanonical: "cron.my-cron",
	}

	pp := &pipeline.Pipeline{
		ID:   1,
		Name: "test-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Name: "my-cron", Type: "cron", Canonical: "cron.my-cron"},
		},
		ResourceTypes: []restype.ResourceType{
			{
				// Mirrors the real built-in cron resource type: Check
				// defaults to the "exec" runner, exactly like git/fs/artifact.
				ID: 1, Name: "cron",
				Check: &utils.RunnerCommand{
					Runner: "exec",
					Args:   []string{"-ec", `printf "[{\"date\":\"now\"}]\n"`},
					Params: map[string]string{"path": "/bin/sh"},
				},
			},
		},
		Runners: []runner.Runner{
			// The pipeline-wide ban from the discussion's proposed workaround.
			{Name: "exec", Run: utils.RunCommand{Path: "/bin/sh", Args: []string{"-ec", "exit 2"}}},
		},
	}
	cwd := t.TempDir()

	svc.EXPECT().ListResourceVersions(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", (*uint32)(nil), (*uint32)(nil), uint32(0)).
		Return([]*resource.Version{}, false, nil).AnyTimes()

	// The real check command (printf ... | would report a new version) never
	// runs, so no new version should ever be created.
	svc.EXPECT().CreateResourceVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	// Instead, the resource's logs should record the check failure.
	svc.EXPECT().UpdateResourceCheckLogs(gomock.Any(), m.TeamCanonical, m.PipelineCanonical, "cron.my-cron", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, logs string) error {
			assert.NotEmpty(t, logs, "resource logs should record the banned-runner failure")
			return nil
		}).AnyTimes()

	w.processResourceCheck(ctx, m, cwd, pp)
}

// TestRunnerOverride_BannedExecRunner_BreaksRealGitResourceType directly
// answers the follow-up question: does banning "exec" pipeline-wide also
// break the real built-in "git" resource type (not just a stand-in like
// cron)? applyRunnerOverride always ends by resolving the runner via
// pp.Runner(runnerName) (worker/service.go:248), which checks the pipeline's
// own Runners before falling back to builtin.Runners() — so yes: git's
// check, pull (get), and push (put) commands all resolve to the banned
// override, exactly like cron's check does.
func TestRunnerOverride_BannedExecRunner_BreaksRealGitResourceType(t *testing.T) {
	git, ok := builtin.ResourceTypes()["git"]
	require.True(t, ok, "the real built-in git resource type must exist")
	require.NotNil(t, git.Check, "git should have a check command")
	require.NotNil(t, git.Pull, "git should have a pull (get) command")
	require.NotNil(t, git.Push, "git should have a push (put) command")
	require.Equal(t, "exec", git.Check.Runner, "git check should default to the exec runner")
	require.Equal(t, "exec", git.Pull.Runner, "git pull should default to the exec runner")
	require.Equal(t, "exec", git.Push.Runner, "git push should default to the exec runner")

	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "exec", Run: utils.RunCommand{Path: "/bin/sh", Args: []string{"-ec", "exit 2"}}},
		},
	}

	for _, tc := range []struct {
		name string
		cmd  *utils.RunnerCommand
	}{
		{"check", git.Check},
		{"pull (get)", git.Pull},
		{"push (put)", git.Push},
	} {
		ru, _, ok := applyRunnerOverride(pp, tc.cmd, git.Runner)
		require.True(t, ok, "%s should still resolve a runner", tc.name)
		assert.Equal(t, "/bin/sh", ru.Run.Path, "git %s should resolve to the banned override, not the real exec passthrough", tc.name)
		assert.Equal(t, []string{"-ec", "exit 2"}, ru.Run.Args, "git %s should resolve to the banned override's args", tc.name)
	}
}
