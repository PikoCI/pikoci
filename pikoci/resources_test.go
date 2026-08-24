package pikoci_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
)

func TestCreateResourceVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().CreateVersion(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(uint32(1), nil)

	v, err := s.S.CreateResourceVersion(ctx, "main", "my-pipeline", "git.repo", resource.Version{
		Version: map[string]interface{}{"ref": "abc123"},
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), v.ID)
}

func TestCreateResourceVersion_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateResourceVersion(ctx, "INVALID", "my-pipeline", "git.repo", resource.Version{})
	require.Error(t, err)

	_, err = s.S.CreateResourceVersion(ctx, "main", "INVALID", "git.repo", resource.Version{})
	require.Error(t, err)

	_, err = s.S.CreateResourceVersion(ctx, "main", "my-pipeline", "INVALID", resource.Version{})
	require.Error(t, err)
}

func TestListResourceVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// limit=0 fetches all, DB returns DESC order
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 2},
		{ID: 1},
	}, nil)
	s.Builds.EXPECT().AggregateStatusByVersionIDs(ctx, []uint32{2, 1}).Return(nil, nil)

	vers, hasMore, err := s.S.ListResourceVersions(ctx, "main", "my-pipeline", "git.repo", nil, nil, 0)
	require.NoError(t, err)
	require.Len(t, vers, 2)
	assert.False(t, hasMore)
	// Newest first
	assert.Equal(t, uint32(2), vers[0].ID)
	assert.Equal(t, uint32(1), vers[1].ID)
}

func TestListResourceVersions_WithLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(3)).Return([]*resource.Version{
		{ID: 5},
		{ID: 4},
		{ID: 3},
	}, nil)
	s.Builds.EXPECT().AggregateStatusByVersionIDs(ctx, []uint32{5, 4}).Return(nil, nil)

	vers, hasMore, err := s.S.ListResourceVersions(ctx, "main", "my-pipeline", "git.repo", nil, nil, 2)
	require.NoError(t, err)
	require.Len(t, vers, 2)
	assert.True(t, hasMore)
}

func TestListResourceVersions_After(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	after := uint32(3)
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), &after, uint32(0)).Return([]*resource.Version{
		{ID: 4},
		{ID: 5},
	}, nil)
	s.Builds.EXPECT().AggregateStatusByVersionIDs(ctx, []uint32{5, 4}).Return(nil, nil)

	vers, hasMore, err := s.S.ListResourceVersions(ctx, "main", "my-pipeline", "git.repo", nil, &after, 0)
	require.NoError(t, err)
	require.Len(t, vers, 2)
	assert.False(t, hasMore)
	// Reversed to newest-first
	assert.Equal(t, uint32(5), vers[0].ID)
	assert.Equal(t, uint32(4), vers[1].ID)
}

func TestListResourceVersions_WithStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 3},
		{ID: 2},
		{ID: 1},
	}, nil)
	s.Builds.EXPECT().AggregateStatusByVersionIDs(ctx, []uint32{3, 2, 1}).Return(map[uint32]string{
		3: "started",
		1: "succeeded",
	}, nil)

	vers, _, err := s.S.ListResourceVersions(ctx, "main", "my-pipeline", "git.repo", nil, nil, 0)
	require.NoError(t, err)
	require.Len(t, vers, 3)
	assert.Equal(t, "started", vers[0].Status)
	assert.Equal(t, "", vers[1].Status)
	assert.Equal(t, "succeeded", vers[2].Status)
}

func TestListPipelineResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Filter(ctx, "main", "my-pipeline").Return([]*resource.Resource{
		{Name: "res1", Canonical: "res1"},
		{Name: "res2", Canonical: "res2"},
	}, nil)
	s.Resources.EXPECT().LatestVersionByResources(ctx, "main", "my-pipeline").Return(map[string]*resource.Version{
		"res1": {ID: 10, Version: map[string]interface{}{"ref": "abc"}},
	}, nil)
	s.Builds.EXPECT().AggregateStatusByVersionIDs(ctx, []uint32{10}).Return(map[uint32]string{10: "succeeded"}, nil)

	rs, err := s.S.ListPipelineResources(ctx, "main", "my-pipeline")
	require.NoError(t, err)
	assert.Len(t, rs, 2)
	require.NotNil(t, rs[0].LatestVersion)
	assert.Equal(t, uint32(10), rs[0].LatestVersion.ID)
	assert.Equal(t, "succeeded", rs[0].LatestVersion.Status)
	assert.Nil(t, rs[1].LatestVersion)
}

func TestListPipelineResources_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.ListPipelineResources(ctx, "INVALID", "my-pipeline")
	require.Error(t, err)

	_, err = s.S.ListPipelineResources(ctx, "main", "INVALID")
	require.Error(t, err)
}

func TestGetPipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &resource.Resource{ID: 1, Canonical: "git.repo"}
	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(expected, nil)

	r, err := s.S.GetPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
	assert.Equal(t, expected, r)
}

func TestUpdatePipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Update(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(nil)

	err := s.S.UpdatePipelineResource(ctx, "main", "my-pipeline", "git.repo", resource.Resource{})
	require.NoError(t, err)
}

func TestTriggerPipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(&resource.Resource{
		ID: 1, Canonical: "git.repo",
	}, nil)

	// UpdatePipelineResource is called to set LastCheck
	s.Resources.EXPECT().Update(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(nil)

	err := s.S.TriggerPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
}

func TestPinResourceVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// validateResourceVersion: FilterVersions returns versions including the one we pin
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
		{ID: 20},
	}, nil)
	s.Resources.EXPECT().PinVersion(ctx, "main", "my-pipeline", "git.repo", uint32(10)).Return(nil)
	// cancelMismatchedPendingBuilds: find pipeline, then filter builds for matching jobs
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
		},
	}, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Pending}).Return([]*build.Build{
		{ID: 1, BuildNumber: "1", Status: build.Pending, ResourceCanonical: "git.repo", VersionID: 20},
	}, nil)
	// The mismatched pending build (version 20 != pinned 10) should be cancelled
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).Return(nil)

	err := s.S.PinResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestPinResourceVersion_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.PinResourceVersion(ctx, "INVALID", "my-pipeline", "git.repo", 10)
	require.Error(t, err)

	err = s.S.PinResourceVersion(ctx, "main", "INVALID", "git.repo", 10)
	require.Error(t, err)

	err = s.S.PinResourceVersion(ctx, "main", "my-pipeline", "INVALID", 10)
	require.Error(t, err)
}

func TestPinResourceVersion_VersionNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// FilterVersions returns versions that don't include the requested one
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 20},
		{ID: 30},
	}, nil)

	err := s.S.PinResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to resource")
}

func TestUnpinResourceVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().UnpinVersion(ctx, "main", "my-pipeline", "git.repo").Return(nil)

	err := s.S.UnpinResourceVersion(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
}

func TestUnpinResourceVersion_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.UnpinResourceVersion(ctx, "INVALID", "my-pipeline", "git.repo")
	require.Error(t, err)

	err = s.S.UnpinResourceVersion(ctx, "main", "INVALID", "git.repo")
	require.Error(t, err)

	err = s.S.UnpinResourceVersion(ctx, "main", "my-pipeline", "INVALID")
	require.Error(t, err)
}

func TestTriggerResourceVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// validateResourceVersion
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	// Find pipeline to iterate jobs
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
		},
	}, nil)
	// FindVersionByID for on_trigger hooks (pipeline has no Raw, so hooks are skipped)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(10)).Return(nil, "", assert.AnError)
	// CreateJobBuild for the matching job
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job"}, nil)
	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "my-job", gomock.Any()).Return(uint32(1), "1", nil)

	err := s.S.TriggerResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestTriggerResourceVersion_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.TriggerResourceVersion(ctx, "INVALID", "my-pipeline", "git.repo", 10)
	require.Error(t, err)

	err = s.S.TriggerResourceVersion(ctx, "main", "INVALID", "git.repo", 10)
	require.Error(t, err)

	err = s.S.TriggerResourceVersion(ctx, "main", "my-pipeline", "INVALID", 10)
	require.Error(t, err)
}

func TestTriggerResourceVersion_SkipsPausedJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// validateResourceVersion
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	// Pipeline has one paused job with a matching get step
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name:   "paused-job",
				Paused: true,
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
		},
	}, nil)
	// FindVersionByID for on_trigger hooks (pipeline has no Raw, so hooks are skipped)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(10)).Return(nil, "", assert.AnError)
	// No builds or sends should happen for paused jobs

	err := s.S.TriggerResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestWebhookTrigger(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FindByWebhookToken(ctx, "my-token").Return(&resource.Resource{
		ID: 1, Canonical: "git.repo",
	}, "main", "my-pipeline", nil)
	// TriggerPipelineResource chain: Find, Notify, Update
	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(&resource.Resource{
		ID: 1, Canonical: "git.repo",
	}, nil)
	s.Resources.EXPECT().Update(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(nil)

	err := s.S.WebhookTrigger(ctx, "my-token")
	require.NoError(t, err)
}

func TestWebhookTrigger_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FindByWebhookToken(ctx, "bad-token").Return(nil, "", "", assert.AnError)

	err := s.S.WebhookTrigger(ctx, "bad-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find Resource by webhook token")
}

func TestRegenerateWebhookToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(&resource.Resource{
		ID: 1, Canonical: "git.repo", WebhookToken: "old-token",
	}, nil)
	s.Resources.EXPECT().Update(ctx, "main", "my-pipeline", "git.repo", gomock.Any()).Return(nil)

	token, err := s.S.RegenerateWebhookToken(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.NotEqual(t, "old-token", token)
	assert.True(t, strings.HasPrefix(token, "git.repo_"), "token should start with resource name prefix")
}

func TestRegenerateWebhookToken_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.RegenerateWebhookToken(ctx, "INVALID", "my-pipeline", "git.repo")
	require.Error(t, err)

	_, err = s.S.RegenerateWebhookToken(ctx, "main", "INVALID", "git.repo")
	require.Error(t, err)

	_, err = s.S.RegenerateWebhookToken(ctx, "main", "my-pipeline", "INVALID")
	require.Error(t, err)
}

func TestGetPipelineResource_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetPipelineResource(ctx, "INVALID", "my-pipeline", "git.repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")

	_, err = s.S.GetPipelineResource(ctx, "main", "INVALID", "git.repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")

	_, err = s.S.GetPipelineResource(ctx, "main", "my-pipeline", "INVALID")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Resource Canonical")
}

func TestUpdatePipelineResource_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.UpdatePipelineResource(ctx, "INVALID", "my-pipeline", "git.repo", resource.Resource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")

	err = s.S.UpdatePipelineResource(ctx, "main", "INVALID", "git.repo", resource.Resource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")

	err = s.S.UpdatePipelineResource(ctx, "main", "my-pipeline", "INVALID", resource.Resource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Resource Canonical")
}

func TestListResourceVersions_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, _, err := s.S.ListResourceVersions(ctx, "INVALID", "my-pipeline", "git.repo", nil, nil, 0)
	require.Error(t, err)

	_, _, err = s.S.ListResourceVersions(ctx, "main", "INVALID", "git.repo", nil, nil, 0)
	require.Error(t, err)

	_, _, err = s.S.ListResourceVersions(ctx, "main", "my-pipeline", "INVALID", nil, nil, 0)
	require.Error(t, err)
}

func TestTriggerPipelineResource_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.TriggerPipelineResource(ctx, "INVALID", "my-pipeline", "git.repo")
	require.Error(t, err)

	err = s.S.TriggerPipelineResource(ctx, "main", "INVALID", "git.repo")
	require.Error(t, err)

	err = s.S.TriggerPipelineResource(ctx, "main", "my-pipeline", "INVALID")
	require.Error(t, err)
}

func TestTriggerPipelineResource_ResourceNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(nil, assert.AnError)

	err := s.S.TriggerPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find Resource")
}

func TestTriggerResourceVersion_SkipsPassedConstraints(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// validateResourceVersion
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	// Pipeline has a job with passed constraints (should be skipped)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "downstream-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo", Passed: []string{"upstream"}}},
				},
			},
		},
	}, nil)
	// FindVersionByID for on_trigger hooks (pipeline has no Raw, so hooks are skipped)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(10)).Return(nil, "", assert.AnError)
	// No builds should be created since the get has passed constraints

	err := s.S.TriggerResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestTriggerResourceVersion_SkipsNonMatchingResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	// Pipeline has a job with a different resource
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "other-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
		},
	}, nil)
	// FindVersionByID for on_trigger hooks (pipeline has no Raw, so hooks are skipped)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(10)).Return(nil, "", assert.AnError)
	// No builds should be created

	err := s.S.TriggerResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestTriggerResourceVersion_SkipsTaskSteps(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	// Pipeline has a job with only task steps (no get)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "task-only",
				Plan: []job.PlanStep{
					{Type: job.StepTypeTask},
				},
			},
		},
	}, nil)
	// FindVersionByID for on_trigger hooks (pipeline has no Raw, so hooks are skipped)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(10)).Return(nil, "", assert.AnError)

	err := s.S.TriggerResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestCancelMismatchedPendingBuilds_NoMismatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Pin to version 10, the pending build also has version 10 — no cancel
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	s.Resources.EXPECT().PinVersion(ctx, "main", "my-pipeline", "git.repo", uint32(10)).Return(nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
		},
	}, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(0), []build.Status{build.Pending}).Return([]*build.Build{
		{ID: 1, BuildNumber: "1", Status: build.Pending, ResourceCanonical: "git.repo", VersionID: 10},
	}, nil)
	// No Update call — build matches pinned version

	err := s.S.PinResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestCancelMismatchedPendingBuilds_JobDoesNotUseResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 10},
	}, nil)
	s.Resources.EXPECT().PinVersion(ctx, "main", "my-pipeline", "git.repo", uint32(10)).Return(nil)
	// Job uses a different resource — should be skipped entirely
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Jobs: []job.Job{
			{
				Name: "other-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
		},
	}, nil)
	// No Filter or Update calls for this job

	err := s.S.PinResourceVersion(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
}

func TestRegenerateWebhookToken_FindError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(nil, assert.AnError)

	_, err := s.S.RegenerateWebhookToken(ctx, "main", "my-pipeline", "git.repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find Resource")
}
