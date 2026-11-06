package pikoci_test

import (
	"context"
	"testing"
	"time"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNextWork_CheckIntervalFallback(t *testing.T) {
	tests := []struct {
		name          string
		serverDefault string
		resourceType  string
		resourceSpec  string
		want          time.Duration
	}{
		{name: "built-in default", want: time.Minute},
		{name: "server default", serverDefault: "@every 6h", want: 6 * time.Hour},
		{name: "resource overrides server default", serverDefault: "@every 6h", resourceSpec: "@every 5m", want: 5 * time.Minute},
		{name: "cron ignores server default", serverDefault: "@every 6h", resourceType: "cron", want: time.Minute},
		{name: "cron keeps its own interval", serverDefault: "@every 6h", resourceType: "cron", resourceSpec: "@every 2h", want: 2 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			s := newService(ctrl)
			ctx := context.TODO()
			s.P.DefaultCheckInterval = tt.serverDefault

			s.Pipelines.EXPECT().FilterAll(ctx).Return(nil, nil)
			s.Resources.EXPECT().FilterDueResources(ctx).Return([]*resource.ResourceWithPipeline{{
				Resource:          resource.Resource{Canonical: "git.repo", Type: tt.resourceType, CheckInterval: tt.resourceSpec},
				TeamCanonical:     "main",
				PipelineCanonical: "p",
			}}, nil)
			s.Resources.EXPECT().ClaimResourceCheck(ctx, "main", "p", "git.repo", gomock.Any(), gomock.Any(), gomock.Any(), uint32(0)).DoAndReturn(
				func(_ context.Context, _, _, _ string, _, lastCheck, nextCheck time.Time, _ uint32) (bool, error) {
					assert.WithinDuration(t, lastCheck.Add(tt.want), nextCheck, time.Second)
					return true, nil
				})

			item, err := s.P.NextWork(ctx, workitem.WorkerContext{})
			require.NoError(t, err)
			require.NotNil(t, item)
		})
	}
}

func TestCreatePipeline_ResourceWithoutIntervalUsesServerDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	s.P.DefaultCheckInterval = "@every 6h"

	hclConfig := []byte(`
resource "git" "repo" {
  params {
    url  = "https://example.com/repo.git"
    name = "repo"
  }
}

job "test" {
  get "git" "repo" { trigger = true }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "p", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, r resource.Resource) (uint32, error) {
			assert.Empty(t, r.CheckInterval, "the default must not be stored, so changing the flag reaches this resource")
			assert.WithinDuration(t, time.Now().Add(6*time.Hour), r.NextCheck, 5*time.Second)
			return 1, nil
		})
	s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(&pipeline.Pipeline{ID: 1, Name: "p", Canonical: "p"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "p", hclConfig, nil)
	require.NoError(t, err)
}

func TestUpdatePipeline_ServerDefaultDoesNotRescheduleResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	s.P.DefaultCheckInterval = "@every 6h"

	hclConfig := []byte(`
resource "git" "repo" {
  params {
    url  = "https://example.com/repo.git"
    name = "repo"
  }
}

job "test" {
  get "git" "repo" { trigger = true }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	next := time.Now().Add(time.Minute).Truncate(time.Second)
	existing := &pipeline.Pipeline{
		ID: 1, Name: "p", Canonical: "p",
		Jobs:      []job.Job{{ID: 1, Name: "test"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "git.repo", Name: "repo", Type: "git", NextCheck: next}},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, "main", "p", gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, "main", "p", "test", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, "main", "p", "git.repo", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _, _ string, r resource.Resource) error {
			assert.Empty(t, r.CheckInterval)
			assert.True(t, r.NextCheck.Equal(next), "saving a pipeline must not reschedule its resource")
			return nil
		})

	_, err := s.S.UpdatePipeline(ctx, "main", "p", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_CronResourceIgnoresServerDefault(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	s.P.DefaultCheckInterval = "@every 6h"

	hclConfig := []byte(`
resource "cron" "timer" {}

job "test" {
  get "cron" "timer" { trigger = true }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "p", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "p", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, r resource.Resource) (uint32, error) {
			assert.WithinDuration(t, time.Now().Add(time.Minute), r.NextCheck, 5*time.Second)
			return 1, nil
		})
	s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(&pipeline.Pipeline{ID: 1, Name: "p", Canonical: "p"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "p", hclConfig, nil)
	require.NoError(t, err)
}

func TestPipelineResources_EffectiveCheckInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	s.P.DefaultCheckInterval = "@every 6h"

	git := func() *resource.Resource { return &resource.Resource{Canonical: "git.repo", Type: "git"} }
	cron := &resource.Resource{Canonical: "cron.timer", Type: "cron"}
	explicit := &resource.Resource{Canonical: "git.fast", Type: "git", CheckInterval: "@every 5m"}

	s.Resources.EXPECT().Find(ctx, "main", "p", "git.repo").Return(git(), nil)
	r, err := s.P.GetPipelineResource(ctx, "main", "p", "git.repo")
	require.NoError(t, err)
	assert.Equal(t, "@every 6h", r.EffectiveCheckInterval)
	assert.Empty(t, r.CheckInterval)

	s.Resources.EXPECT().Filter(ctx, "main", "p").Return([]*resource.Resource{git(), cron, explicit}, nil)
	s.Resources.EXPECT().LatestVersionByResources(ctx, "main", "p").Return(nil, nil)
	rs, err := s.P.ListPipelineResources(ctx, "main", "p")
	require.NoError(t, err)
	assert.Equal(t, "@every 6h", rs[0].EffectiveCheckInterval)
	assert.Equal(t, "@every 1m", rs[1].EffectiveCheckInterval)
	assert.Equal(t, "@every 5m", rs[2].EffectiveCheckInterval)
}
