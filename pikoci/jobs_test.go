package pikoci_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/queue"
	"github.com/xescugc/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func TestGetPipelineJob_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetPipelineJob(ctx, "INVALID", "pp", "jn")
	require.Error(t, err)

	_, err = s.S.GetPipelineJob(ctx, "main", "INVALID", "jn")
	require.Error(t, err)

	_, err = s.S.GetPipelineJob(ctx, "main", "pp", "INVALID")
	require.Error(t, err)
}

func TestTriggerPipelineJob_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.TriggerPipelineJob(ctx, "INVALID", "pp", "jn")
	require.Error(t, err)

	err = s.S.TriggerPipelineJob(ctx, "main", "INVALID", "jn")
	require.Error(t, err)

	err = s.S.TriggerPipelineJob(ctx, "main", "pp", "INVALID")
	require.Error(t, err)
}

func TestGetPipelineJob_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(nil, assert.AnError)

	_, err := s.S.GetPipelineJob(ctx, "main", "pp", "jn")
	require.Error(t, err)
}

func TestTriggerPipelineJob_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(nil, assert.AnError)

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn")
	require.Error(t, err)
}

func TestTriggerPipelineJob_SendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{ID: 1}, nil)
	// TriggerPipelineJob now creates a pending build first
	s.Builds.EXPECT().Create(ctx, "main", "pp", "jn", gomock.Any()).Return(uint32(1), "1", nil)
	s.Topic.EXPECT().Send(ctx, gomock.Any()).Return(assert.AnError)

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to Trigger Job")
}

func TestTriggerPipelineJob_PinsLatestVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	ppc := "pp"
	jn := "jn"

	j := &job.Job{
		ID:   1,
		Name: jn,
		Plan: []job.PlanStep{
			{
				Type: job.StepTypeGet,
				Get:  &job.GetStep{Type: "git", Name: "my-repo", Trigger: true},
			},
		},
	}

	versions := []*resource.Version{
		{ID: 30},
		{ID: 20},
		{ID: 10},
	}

	rCan := j.GetSteps()[0].ResourceCanonical()

	s.Jobs.EXPECT().Find(ctx, tc, ppc, jn).Return(j, nil)
	s.Resources.EXPECT().FilterVersions(ctx, tc, ppc, rCan, (*uint32)(nil), (*uint32)(nil), uint32(0)).Return(versions, nil)
	// TriggerPipelineJob now creates a pending build first
	s.Builds.EXPECT().Create(ctx, tc, ppc, jn, gomock.Any()).Return(uint32(5), "1", nil)

	s.Topic.EXPECT().Send(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, msg *pubsub.Message) error {
		var body queue.Body
		err := json.Unmarshal(msg.Body, &body)
		require.NoError(t, err)
		assert.Equal(t, tc, body.TeamCanonical)
		assert.Equal(t, ppc, body.PipelineCanonical)
		assert.Equal(t, jn, body.JobName)
		assert.Equal(t, rCan, body.ResourceCanonical)
		assert.Equal(t, uint32(30), body.VersionID)
		assert.Equal(t, uint32(5), body.BuildID)
		return nil
	})

	err := s.S.TriggerPipelineJob(ctx, tc, ppc, jn)
	require.NoError(t, err)
}
