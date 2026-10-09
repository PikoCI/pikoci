package pikoci

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
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

// ─── ReachableJobs ────────────────────────────────────────────────────────────

func TestReachableJobs_Empty(t *testing.T) {
	p := &pipeline.Pipeline{}
	assert.Empty(t, ReachableJobs(p, "git.repo"))
}

func TestReachableJobs_DirectTrigger(t *testing.T) {
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
	}}
	result := ReachableJobs(p, "git.repo")
	require.Len(t, result, 1)
	assert.Equal(t, "build", result[0].Name)
}

func TestReachableJobs_TriggerFalse_Excluded(t *testing.T) {
	// A get step with trigger=false on the triggering resource is not a direct trigger.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", false)),
	}}
	assert.Empty(t, ReachableJobs(p, "git.repo"))
}

func TestReachableJobs_WrongResource_Excluded(t *testing.T) {
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("cron", "timer", true)),
	}}
	assert.Empty(t, ReachableJobs(p, "git.repo"))
}

func TestReachableJobs_PassedConstraint_NotDirectTrigger(t *testing.T) {
	// A get that has passed constraints is not a direct trigger, even if trigger=true.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("test", makeGetStep("git", "repo", true, "build")),
	}}
	assert.Empty(t, ReachableJobs(p, "git.repo"))
}

func TestReachableJobs_Transitive(t *testing.T) {
	// build triggers directly; test depends on build via passed.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("build", makeGetStep("git", "repo", true)),
		makeJob("test", makeGetStep("git", "repo", false, "build")),
	}}
	result := ReachableJobs(p, "git.repo")
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
	result := ReachableJobs(p, "git.repo")
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
	result := ReachableJobs(p, "git.repo")
	require.Len(t, result, 1)
	assert.Equal(t, "build", result[0].Name)
}

func TestReachableJobs_DeclarationOrder(t *testing.T) {
	// Jobs declared in reverse dependency order are returned in declaration order.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("deploy", makeGetStep("git", "repo", false, "build")),
		makeJob("build", makeGetStep("git", "repo", true)),
	}}
	result := ReachableJobs(p, "git.repo")
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
	result := ReachableJobs(p, "git.repo")
	require.Len(t, result, 2)
	names := map[string]bool{result[0].Name: true, result[1].Name: true}
	assert.True(t, names["backend"])
	assert.True(t, names["frontend"])
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

// ─── ReachableJobs — production topology (3 direct + 1 downstream) ───────────

func TestReachableJobs_ThreeDirectTriggers_DownstreamWithMultiplePassed(t *testing.T) {
	// Mirrors the pikoci deploy pipeline: backend, frontend, test-integration
	// are all direct triggers; test-backends depends on all three via passed.
	p := &pipeline.Pipeline{Jobs: []job.Job{
		makeJob("backend", makeGetStep("git", "pikoci_pr", true)),
		makeJob("frontend", makeGetStep("git", "pikoci_pr", true)),
		makeJob("test-integration", makeGetStep("git", "pikoci_pr", true)),
		makeJob("test-backends", makeGetStep("git", "pikoci_pr", true, "backend", "frontend", "test-integration")),
	}}
	result := ReachableJobs(p, "git.pikoci_pr")
	require.Len(t, result, 4)
	names := make(map[string]bool, 4)
	for _, j := range result {
		names[j.Name] = true
	}
	assert.True(t, names["backend"], "backend must be reachable")
	assert.True(t, names["frontend"], "frontend must be reachable")
	assert.True(t, names["test-integration"], "test-integration must be reachable")
	assert.True(t, names["test-backends"], "test-backends must be reachable via BFS")
}

func TestReadPipeline_DownstreamJobWithPassed_HasOnTrigger(t *testing.T) {
	// Verifies that ReadPipeline correctly populates OnTrigger for a job that
	// has both trigger=true and passed constraints on its get step.
	raw := []byte(`
resource "git" "pikoci_pr" {}
notification_type "recorder" {
  notify "exec" { path = "/bin/true" }
}
notification "recorder" "ci" {}
job "backend" {
  get "git" "pikoci_pr" { trigger = true }
  on_trigger {
    notify "recorder" "ci" {}
  }
}
job "test-backends" {
  get "git" "pikoci_pr" {
    trigger = true
    passed  = ["backend"]
  }
  on_trigger {
    notify "recorder" "ci" {}
  }
}
`)
	pp, err := ReadPipeline(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Len(t, pp.Jobs, 2)

	jobsByName := make(map[string]job.Job)
	for _, j := range pp.Jobs {
		jobsByName[j.Name] = j
	}

	tb, ok := jobsByName["test-backends"]
	require.True(t, ok, "test-backends job must exist in parsed pipeline")
	require.Len(t, tb.OnTrigger, 1, "test-backends must have on_trigger populated by ReadPipeline")
	assert.Equal(t, job.StepTypeNotify, tb.OnTrigger[0].Type)

	backend, ok := jobsByName["backend"]
	require.True(t, ok)
	require.Len(t, backend.OnTrigger, 1, "backend must have on_trigger populated by ReadPipeline")
}

// ─── ReachableJobs — outer-break branch ──────────────────────────────────────

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
	result := ReachableJobs(p, "git.repo")
	require.Len(t, result, 2)
	assert.Equal(t, "build", result[0].Name)
	assert.Equal(t, "test", result[1].Name)
}
