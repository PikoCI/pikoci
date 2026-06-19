package pikoci_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"go.uber.org/mock/gomock"
)

func TestGetResourceVersionPath_SimpleChain(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	tc := "main"
	pn := "my-pipeline"

	pp := &pipeline.Pipeline{
		Canonical: pn,
		Jobs: []job.Job{
			{
				Name: "build",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo", Trigger: true}},
				},
			},
			{
				Name: "test",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo", Passed: []string{"build"}}},
				},
			},
			{
				Name: "deploy",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo", Passed: []string{"test"}}},
				},
			},
		},
		Resources: []resource.Resource{
			{Canonical: "git.repo", Type: "git", Name: "repo"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), tc, pn).Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(42)).Return(
		&resource.Version{ID: 42, Version: map[string]interface{}{"ref": "abc123"}},
		"git.repo",
		nil,
	)
	// Round 1: all 3 jobs searched with version 42 - build and test found
	s.Builds.EXPECT().FindByVersionAndJobs(gomock.Any(), tc, pn, uint32(42), []string{"build", "test", "deploy"}).Return(
		map[string][]*build.Build{
			"build": {{ID: 1, BuildNumber: "1", Status: build.Succeeded}},
			"test":  {{ID: 2, BuildNumber: "2", Status: build.Started}},
		},
		nil,
	)
	// Chain walk: get versions from found builds
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(1)).Return(map[string]uint32{"repo": 42}, nil)
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(2)).Return(map[string]uint32{"repo": 42}, nil)
	// No new version IDs found, so no round 2 needed (version 42 already seen)

	resp, err := s.S.GetResourceVersionPath(ctx, tc, pn, "git.repo", 42)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "git.repo", resp.Resource.Canonical)
	assert.Equal(t, "abc123", resp.Resource.Version["ref"])
	assert.Equal(t, 3, resp.Total)
	assert.Equal(t, 1, resp.Completed) // only "build" is terminal (succeeded)
	require.Len(t, resp.Path, 3)
	assert.Equal(t, "build", resp.Path[0].JobName)
	assert.NotNil(t, resp.Path[0].Build)
	assert.Equal(t, build.Succeeded, resp.Path[0].Build.Status)
	assert.Equal(t, "test", resp.Path[1].JobName)
	assert.NotNil(t, resp.Path[1].Build)
	assert.Equal(t, build.Started, resp.Path[1].Build.Status)
	assert.Equal(t, "deploy", resp.Path[2].JobName)
	assert.Nil(t, resp.Path[2].Build) // not yet triggered
}

func TestGetResourceVersionPath_IntermediateResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	tc := "main"
	pn := "my-pipeline"

	// Pipeline: gen gets cron.timer, puts artifact.output → deploy-staging gets artifact.output (passed: [gen])
	pp := &pipeline.Pipeline{
		Canonical: pn,
		Jobs: []job.Job{
			{
				Name: "gen",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer", Trigger: true}},
					{Type: job.StepTypePut, Put: &job.PutStep{Type: "artifact", Name: "output"}},
				},
			},
			{
				Name: "deploy-staging",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "artifact", Name: "output", Passed: []string{"gen"}}},
				},
			},
		},
		Resources: []resource.Resource{
			{Canonical: "cron.timer", Type: "cron", Name: "timer"},
			{Canonical: "artifact.output", Type: "artifact", Name: "output"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), tc, pn).Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(1)).Return(
		&resource.Version{ID: 1, Version: map[string]interface{}{"date": "today"}},
		"cron.timer",
		nil,
	)

	// Round 1: search gen and deploy-staging for version 1
	// Only gen consumed version 1 directly
	s.Builds.EXPECT().FindByVersionAndJobs(gomock.Any(), tc, pn, uint32(1), []string{"gen", "deploy-staging"}).Return(
		map[string][]*build.Build{
			"gen": {{ID: 10, BuildNumber: "1", Status: build.Succeeded}},
		},
		nil,
	)
	// Chain walk: gen's build_get_versions has both the cron version (1) and artifact version (5)
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(10)).Return(
		map[string]uint32{"timer": 1, "output": 5}, nil,
	)
	// Round 2: search deploy-staging for version 5 (the intermediate artifact version)
	s.Builds.EXPECT().FindByVersionAndJobs(gomock.Any(), tc, pn, uint32(5), []string{"deploy-staging"}).Return(
		map[string][]*build.Build{
			"deploy-staging": {{ID: 11, BuildNumber: "1", Status: build.Succeeded}},
		},
		nil,
	)
	// Chain walk: deploy-staging's build_get_versions
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(11)).Return(
		map[string]uint32{"output": 5}, nil,
	)

	resp, err := s.S.GetResourceVersionPath(ctx, tc, pn, "cron.timer", 1)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Path, 2)
	assert.Equal(t, "gen", resp.Path[0].JobName)
	assert.NotNil(t, resp.Path[0].Build)
	assert.Equal(t, "deploy-staging", resp.Path[1].JobName)
	assert.NotNil(t, resp.Path[1].Build)
	assert.Equal(t, 2, resp.Completed)
}

func TestGetResourceVersionPath_EmptyChain(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Canonical: "my-pipeline",
		Jobs:      []job.Job{},
		Resources: []resource.Resource{
			{Canonical: "git.repo", Type: "git", Name: "repo"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), "main", "my-pipeline").Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(10)).Return(
		&resource.Version{ID: 10, Version: map[string]interface{}{"ref": "xyz"}},
		"git.repo",
		nil,
	)

	resp, err := s.S.GetResourceVersionPath(ctx, "main", "my-pipeline", "git.repo", 10)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.Total)
	assert.Empty(t, resp.Path)
}

func TestGetResourceVersionPath_ResourceNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Canonical: "my-pipeline",
		Resources: []resource.Resource{
			{Canonical: "git.repo"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), "main", "my-pipeline").Return(pp, nil)

	_, err := s.S.GetResourceVersionPath(ctx, "main", "my-pipeline", "git.other", 42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in pipeline")
}

func TestGetResourceVersionPath_VersionBelongsToDifferentResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Canonical: "my-pipeline",
		Resources: []resource.Resource{
			{Canonical: "git.repo"},
			{Canonical: "cron.nightly"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), "main", "my-pipeline").Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(1)).Return(
		&resource.Version{ID: 1, Version: map[string]interface{}{"date": "today"}},
		"cron.nightly",
		nil,
	)

	_, err := s.S.GetResourceVersionPath(ctx, "main", "my-pipeline", "git.repo", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "belongs to resource")
}

func TestGetResourceVersionPath_VersionNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Canonical: "my-pipeline",
		Resources: []resource.Resource{
			{Canonical: "git.repo"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), "main", "my-pipeline").Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(999)).Return(
		nil, "", assert.AnError,
	)

	_, err := s.S.GetResourceVersionPath(ctx, "main", "my-pipeline", "git.repo", 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetResourceVersionPath_WithRetries(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	tc := "main"
	pn := "my-pipeline"

	pp := &pipeline.Pipeline{
		Canonical: pn,
		Jobs: []job.Job{
			{
				Name: "build",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
		},
		Resources: []resource.Resource{
			{Canonical: "git.repo"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), tc, pn).Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(5)).Return(
		&resource.Version{ID: 5, Version: map[string]interface{}{"ref": "abc"}},
		"git.repo",
		nil,
	)
	s.Builds.EXPECT().FindByVersionAndJobs(gomock.Any(), tc, pn, uint32(5), []string{"build"}).Return(
		map[string][]*build.Build{
			"build": {
				{ID: 10, BuildNumber: "3", Status: build.Succeeded},
				{ID: 11, BuildNumber: "3.1", Status: build.Failed},
			},
		},
		nil,
	)
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(10)).Return(map[string]uint32{"repo": 5}, nil)
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(11)).Return(map[string]uint32{"repo": 5}, nil)

	resp, err := s.S.GetResourceVersionPath(ctx, tc, pn, "git.repo", 5)
	require.NoError(t, err)
	require.Len(t, resp.Path, 1)
	assert.NotNil(t, resp.Path[0].Build)
	assert.Equal(t, "3", resp.Path[0].Build.BuildNumber)
	require.Len(t, resp.Path[0].Retries, 1)
	assert.Equal(t, "3.1", resp.Path[0].Retries[0].BuildNumber)
	assert.Equal(t, 1, resp.Completed)
}

func TestGetResourceVersionPath_ForEachExpansion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	tc := "main"
	pn := "my-pipeline"

	pp := &pipeline.Pipeline{
		Canonical: pn,
		Jobs: []job.Job{
			{
				Name:         "build-a",
				ForEachGroup: "build-group",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
			{
				Name:         "build-b",
				ForEachGroup: "build-group",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
			{
				Name: "deploy",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo", Passed: []string{"build-group"}}},
				},
			},
		},
		Resources: []resource.Resource{
			{Canonical: "git.repo"},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), tc, pn).Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(1)).Return(
		&resource.Version{ID: 1, Version: map[string]interface{}{"ref": "main"}},
		"git.repo",
		nil,
	)
	s.Builds.EXPECT().FindByVersionAndJobs(gomock.Any(), tc, pn, uint32(1), gomock.Any()).Return(
		map[string][]*build.Build{},
		nil,
	)

	resp, err := s.S.GetResourceVersionPath(ctx, tc, pn, "git.repo", 1)
	require.NoError(t, err)
	require.Len(t, resp.Path, 3)
	assert.Equal(t, "build-a", resp.Path[0].JobName)
	assert.Equal(t, "build-b", resp.Path[1].JobName)
	assert.Equal(t, "deploy", resp.Path[2].JobName)
}

func TestGetResourceVersionPath_InvalidCanonicals(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetResourceVersionPath(ctx, "INVALID", "pipeline", "git.repo", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")

	_, err = s.S.GetResourceVersionPath(ctx, "main", "INVALID", "git.repo", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")
}

func TestGetPublicResourceVersionPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	tc := "main"
	pn := "my-pipeline"

	pp := &pipeline.Pipeline{
		Canonical: pn,
		Jobs: []job.Job{
			{
				Name: "build",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "git", Name: "repo"}},
				},
			},
		},
		Resources: []resource.Resource{
			{Canonical: "git.repo"},
		},
	}

	s.Pipelines.EXPECT().FindPublic(gomock.Any(), tc, pn).Return(pp, nil)
	s.Resources.EXPECT().FindVersionByID(gomock.Any(), uint32(5)).Return(
		&resource.Version{ID: 5, Version: map[string]interface{}{"ref": "abc"}},
		"git.repo",
		nil,
	)
	s.Builds.EXPECT().FindByVersionAndJobs(gomock.Any(), tc, pn, uint32(5), []string{"build"}).Return(
		map[string][]*build.Build{
			"build": {{
				ID: 10, BuildNumber: "1", Status: build.Succeeded,
				Steps: []build.Step{{Name: "get", Logs: "secret-log"}},
				Job:   []build.Step{{Name: "job", Logs: "job-log"}},
			}},
		},
		nil,
	)
	s.Builds.EXPECT().FindGetVersions(gomock.Any(), uint32(10)).Return(map[string]uint32{"repo": 5}, nil)

	resp, err := s.S.GetPublicResourceVersionPath(ctx, tc, pn, "git.repo", 5)
	require.NoError(t, err)
	require.Len(t, resp.Path, 1)
	assert.Empty(t, resp.Path[0].Build.Steps[0].Logs)
	assert.Empty(t, resp.Path[0].Build.Job[0].Logs)
}
