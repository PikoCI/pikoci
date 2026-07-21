package pikoci_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
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

	err := s.S.TriggerPipelineJob(ctx, "INVALID", "pp", "jn", nil, false)
	require.Error(t, err)

	err = s.S.TriggerPipelineJob(ctx, "main", "INVALID", "jn", nil, false)
	require.Error(t, err)

	err = s.S.TriggerPipelineJob(ctx, "main", "pp", "INVALID", nil, false)
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

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn", nil, false)
	require.Error(t, err)
}

func TestTriggerPipelineJob_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{ID: 1}, nil).Times(2)
	s.Builds.EXPECT().Create(ctx, "main", "pp", "jn", gomock.Any()).Return(uint32(1), "1", nil)

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn", nil, false)
	require.NoError(t, err)
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

	s.Jobs.EXPECT().Find(ctx, tc, ppc, jn).Return(j, nil).Times(2)
	// Resource is not pinned — falls through to latest version
	s.Resources.EXPECT().Find(ctx, tc, ppc, rCan).Return(&resource.Resource{}, nil)
	s.Resources.EXPECT().FilterVersions(ctx, tc, ppc, rCan, (*uint32)(nil), (*uint32)(nil), uint32(0)).Return(versions, nil)
	// TriggerPipelineJob creates a pending build and calls Notify()
	s.Builds.EXPECT().Create(ctx, tc, ppc, jn, gomock.Any()).Return(uint32(5), "1", nil)

	err := s.S.TriggerPipelineJob(ctx, tc, ppc, jn, nil, false)
	require.NoError(t, err)
}

func TestTriggerPipelineJob_UsesPinnedVersion(t *testing.T) {
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

	rCan := j.GetSteps()[0].ResourceCanonical()
	pinnedVersion := uint32(20)

	s.Jobs.EXPECT().Find(ctx, tc, ppc, jn).Return(j, nil).Times(2)
	// Resource is pinned to version 20
	s.Resources.EXPECT().Find(ctx, tc, ppc, rCan).Return(&resource.Resource{PinnedVersionID: &pinnedVersion}, nil)
	// FilterVersions should NOT be called — pinned version is used directly
	s.Builds.EXPECT().Create(ctx, tc, ppc, jn, gomock.Any()).Return(uint32(5), "1", nil)

	err := s.S.TriggerPipelineJob(ctx, tc, ppc, jn, nil, false)
	require.NoError(t, err)
}

func TestTriggerPipelineJob_JobPaused(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{ID: 1, Paused: true}, nil)

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn", nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is paused")
}

func TestTriggerPipelineJob_InputsDefaults(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	defVal := "latest"
	j := &job.Job{
		ID:   1,
		Name: "jn",
		Inputs: []job.Input{
			{Name: "version", Type: "string", Default: &defVal},
			{Name: "count", Type: "number", Default: func() *string { s := "3"; return &s }()},
		},
	}

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(j, nil).Times(2)
	s.Builds.EXPECT().Create(ctx, "main", "pp", "jn", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn, jn string, b build.Build) (uint32, string, error) {
			require.NotNil(t, b.InputValues)
			assert.Equal(t, "latest", b.InputValues["version"])
			assert.Equal(t, "3", b.InputValues["count"])
			return uint32(1), "1", nil
		})

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn", nil, false)
	require.NoError(t, err)
}

func TestTriggerPipelineJob_InputsProvided(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	defVal := "latest"
	j := &job.Job{
		ID:   1,
		Name: "jn",
		Inputs: []job.Input{
			{Name: "version", Type: "string", Default: &defVal},
		},
	}

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(j, nil).Times(2)
	s.Builds.EXPECT().Create(ctx, "main", "pp", "jn", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn, jn string, b build.Build) (uint32, string, error) {
			require.NotNil(t, b.InputValues)
			assert.Equal(t, "v2.0", b.InputValues["version"])
			return uint32(1), "1", nil
		})

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn", map[string]string{"version": "v2.0"}, true)
	require.NoError(t, err)
}

func TestTriggerPipelineJob_InputsRequiredMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	j := &job.Job{
		ID:   1,
		Name: "jn",
		Inputs: []job.Input{
			{Name: "version", Type: "string"}, // no default = required
		},
	}

	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(j, nil)

	err := s.S.TriggerPipelineJob(ctx, "main", "pp", "jn", nil, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required input")
}

func TestPauseJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().SetPaused(ctx, "main", "pp", "jn", true).Return(nil)

	err := s.S.PauseJob(ctx, "main", "pp", "jn")
	require.NoError(t, err)
}

func TestPauseJob_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.PauseJob(ctx, "INVALID", "pp", "jn")
	require.Error(t, err)

	err = s.S.PauseJob(ctx, "main", "INVALID", "jn")
	require.Error(t, err)

	err = s.S.PauseJob(ctx, "main", "pp", "INVALID")
	require.Error(t, err)
}

func TestUnpauseJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().SetPaused(ctx, "main", "pp", "jn", false).Return(nil)

	err := s.S.UnpauseJob(ctx, "main", "pp", "jn")
	require.NoError(t, err)
}

func TestUnpauseJob_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.UnpauseJob(ctx, "INVALID", "pp", "jn")
	require.Error(t, err)

	err = s.S.UnpauseJob(ctx, "main", "INVALID", "jn")
	require.Error(t, err)

	err = s.S.UnpauseJob(ctx, "main", "pp", "INVALID")
	require.Error(t, err)
}

func TestPausePipeline_PausesAllJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().PauseAll(ctx, "main", "pp").Return(nil)

	err := s.S.PausePipeline(ctx, "main", "pp")
	require.NoError(t, err)
}

func TestUnpausePipeline_UnpausesAllJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().UnpauseAll(ctx, "main", "pp").Return(nil)

	err := s.S.UnpausePipeline(ctx, "main", "pp")
	require.NoError(t, err)
}
