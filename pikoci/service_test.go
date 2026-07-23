package pikoci_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
)

func TestCreatePipeline(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppc := "pipeline-name"

	b, err := os.ReadFile("testdata/pipeline.hcl")
	require.NoError(t, err)

	mvars := map[string]interface{}{
		"repo_name": "repo",
	}

	s.Pipelines.EXPECT().Create(ctx, tc, gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, tc, ppc, gomock.Any()).Return(uint32(1), nil).Times(3)
	s.Resources.EXPECT().Create(ctx, tc, ppc, gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, r resource.Resource) (uint32, error) {
			assert.True(t, strings.HasPrefix(r.WebhookToken, r.Canonical+"_"),
				"webhook token should start with canonical prefix, got %q", r.WebhookToken)
			return uint32(1), nil
		},
	).Times(1)
	// GetPipeline uses Find which now does a single JOIN query
	s.Pipelines.EXPECT().Find(ctx, tc, ppc).Return(&pipeline.Pipeline{Name: ppc}, nil)

	pp, err := s.S.CreatePipeline(ctx, tc, ppc, b, mvars)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestTriggerPipelineJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppc := "pipeline-name"
	jn := "job-name"

	s.Jobs.EXPECT().Find(ctx, tc, ppc, jn).Return(&job.Job{ID: 2}, nil).Times(2)
	// TriggerPipelineJob creates a pending build and calls Notify()
	s.Builds.EXPECT().Create(ctx, tc, ppc, jn, gomock.Any()).Return(uint32(1), "1", nil)

	err := s.S.TriggerPipelineJob(ctx, tc, ppc, jn, nil, false)
	require.NoError(t, err)
}

func TestCreatePipeline_SerialGroups(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppc := "serial-pipeline"

	b, err := os.ReadFile("testdata/serial_groups.hcl")
	require.NoError(t, err)

	var capturedJobs []job.Job
	s.Pipelines.EXPECT().Create(ctx, tc, gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, tc, ppc, gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, j job.Job) (uint32, error) {
			capturedJobs = append(capturedJobs, j)
			return uint32(len(capturedJobs)), nil
		}).Times(2)
	s.Resources.EXPECT().Create(ctx, tc, ppc, gomock.Any()).Return(uint32(1), nil).Times(1)
	s.Pipelines.EXPECT().Find(ctx, tc, ppc).Return(&pipeline.Pipeline{Name: ppc}, nil)

	pp, err := s.S.CreatePipeline(ctx, tc, ppc, b, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	require.Len(t, capturedJobs, 2)
	assert.Equal(t, []string{"deploy"}, capturedJobs[0].SerialGroups)
	assert.Equal(t, []string{"deploy"}, capturedJobs[1].SerialGroups)
}

func TestCreatePipeline_SerialGroups_InvalidName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hcl := []byte(`
resource "git" "repo" {
  params {
    url = "https://example.com/repo.git"
  }
}
job "bad-job" {
  serial_groups = ["INVALID NAME"]
  get "git" "repo" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "team", "pipe", hcl, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid serial_group name")
}

func TestCreatePipeline_ConditionalSteps(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppc := "conditional-pipeline"

	b, err := os.ReadFile("testdata/conditional.hcl")
	require.NoError(t, err)

	var capturedJob job.Job
	s.Pipelines.EXPECT().Create(ctx, tc, gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, tc, ppc, gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, j job.Job) (uint32, error) {
			capturedJob = j
			return uint32(1), nil
		}).Times(1)
	s.Resources.EXPECT().Create(ctx, tc, ppc, gomock.Any()).Return(uint32(1), nil).Times(1)
	s.Pipelines.EXPECT().Find(ctx, tc, ppc).Return(&pipeline.Pipeline{Name: ppc}, nil)

	pp, err := s.S.CreatePipeline(ctx, tc, ppc, b, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)

	// Plan should have: get, task, if (with 3 branches)
	require.Len(t, capturedJob.Plan, 3, "expected 3 top-level plan steps")
	assert.Equal(t, job.StepTypeGet, capturedJob.Plan[0].Type)
	assert.Equal(t, job.StepTypeTask, capturedJob.Plan[1].Type)
	assert.Equal(t, job.StepTypeIf, capturedJob.Plan[2].Type)

	ifStep := capturedJob.Plan[2].If
	require.NotNil(t, ifStep)
	require.Len(t, ifStep.Branches, 3)

	assert.Equal(t, "if", ifStep.Branches[0].Type)
	assert.Equal(t, "check-prod", ifStep.Branches[0].Label)
	assert.Equal(t, "$TASK_DETECT_ENV_ENV == 'production'", ifStep.Branches[0].Condition)
	require.Len(t, ifStep.Branches[0].Steps, 1)
	assert.Equal(t, job.StepTypeTask, ifStep.Branches[0].Steps[0].Type)
	assert.Equal(t, "deploy-prod", ifStep.Branches[0].Steps[0].Task.Name)

	assert.Equal(t, "else_if", ifStep.Branches[1].Type)
	assert.Equal(t, "check-staging", ifStep.Branches[1].Label)
	assert.Equal(t, "$TASK_DETECT_ENV_ENV == 'staging'", ifStep.Branches[1].Condition)
	require.Len(t, ifStep.Branches[1].Steps, 1)
	assert.Equal(t, "deploy-staging", ifStep.Branches[1].Steps[0].Task.Name)

	assert.Equal(t, "else", ifStep.Branches[2].Type)
	assert.Empty(t, ifStep.Branches[2].Condition)
	require.Len(t, ifStep.Branches[2].Steps, 1)
	assert.Equal(t, "skip-deploy", ifStep.Branches[2].Steps[0].Task.Name)

	// FlatPlanSteps should include steps from all branches
	flat := capturedJob.FlatPlanSteps()
	var taskNames []string
	for _, fp := range flat {
		if fp.Type == job.StepTypeTask && fp.Task != nil {
			taskNames = append(taskNames, fp.Task.Name)
		}
	}
	assert.Contains(t, taskNames, "detect-env")
	assert.Contains(t, taskNames, "deploy-prod")
	assert.Contains(t, taskNames, "deploy-staging")
	assert.Contains(t, taskNames, "skip-deploy")
}

func TestGetPipelineJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppc := "pipeline-name"
	jn := "job-name"
	rj := &job.Job{ID: 2}

	s.Jobs.EXPECT().Find(ctx, tc, ppc, jn).Return(rj, nil)

	j, err := s.S.GetPipelineJob(ctx, tc, ppc, jn)
	require.NoError(t, err)
	assert.Equal(t, rj, j)
}
