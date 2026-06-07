package pikoci_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/queue"
	"github.com/pikoci/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
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

	s.Jobs.EXPECT().Find(ctx, tc, ppc, jn).Return(&job.Job{ID: 2}, nil)
	// TriggerPipelineJob now creates a pending build first
	s.Builds.EXPECT().Create(ctx, tc, ppc, jn, gomock.Any()).Return(uint32(1), "1", nil)

	s.Topic.EXPECT().Send(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, tc, body.TeamCanonical)
		assert.Equal(t, ppc, body.PipelineCanonical)
		assert.Equal(t, jn, body.JobName)
		assert.Equal(t, uint32(1), body.BuildID)
		return nil
	})

	err := s.S.TriggerPipelineJob(ctx, tc, ppc, jn)
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
