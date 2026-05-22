package pikoci_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/queue"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func TestCreatePipeline(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppn := "pipeline-name"

	b, err := os.ReadFile("testdata/pipeline.hcl")
	require.NoError(t, err)

	mvars := map[string]interface{}{
		"repo_name": "repo",
	}

	s.Pipelines.EXPECT().Create(ctx, tc, gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, tc, ppn, gomock.Any()).Return(uint32(1), nil).Times(3)
	s.Resources.EXPECT().Create(ctx, tc, ppn, gomock.Any()).Return(uint32(1), nil).Times(1)
	// GetPipeline uses Find which now does a single JOIN query
	s.Pipelines.EXPECT().Find(ctx, tc, ppn).Return(&pipeline.Pipeline{Name: ppn}, nil)

	pp, err := s.S.CreatePipeline(ctx, tc, ppn, b, mvars)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestTriggerPipelineJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppn := "pipeline-name"
	jn := "job-name"

	m := queue.Body{
		TeamCanonical: tc,
		PipelineName:  ppn,
		JobName:       jn,
	}

	mb, err := json.Marshal(m)
	require.NoError(t, err)
	s.Jobs.EXPECT().Find(ctx, tc, ppn, jn).Return(&job.Job{ID: 2}, nil)
	s.Topic.EXPECT().Send(ctx, &pubsub.Message{
		Body: mb,
	}).Return(nil)

	err = s.S.TriggerPipelineJob(ctx, tc, ppn, jn)
	require.NoError(t, err)
}

func TestGetPipelineJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "team-canonical"
	ppn := "pipeline-name"
	jn := "job-name"
	rj := &job.Job{ID: 2}

	s.Jobs.EXPECT().Find(ctx, tc, ppn, jn).Return(rj, nil)

	j, err := s.S.GetPipelineJob(ctx, tc, ppn, jn)
	require.NoError(t, err)
	assert.Equal(t, rj, j)
}
