package pikoci_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"go.uber.org/mock/gomock"
)

func TestGetPipeline(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs:      []job.Job{{ID: 1, Name: "echo"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "cron.my-cron"}},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(expected, nil)

	pp, err := s.S.GetPipeline(ctx, "main", "my-pipeline")
	require.NoError(t, err)
	assert.Equal(t, expected, pp)
}

func TestGetPipeline_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetPipeline(ctx, "INVALID", "my-pipeline")
	require.Error(t, err)

	_, err = s.S.GetPipeline(ctx, "main", "INVALID")
	require.Error(t, err)
}

func TestListPipelines(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := []*pipeline.Pipeline{
		{ID: 1, Name: "pipeline-a"},
		{ID: 2, Name: "pipeline-b"},
	}
	s.Pipelines.EXPECT().Filter(ctx, "main").Return(expected, nil)
	s.Builds.EXPECT().LastBuildAtByPipeline(ctx, "main").Return(map[uint32]time.Time{}, nil)

	pps, err := s.S.ListPipelines(ctx, "main")
	require.NoError(t, err)
	assert.Len(t, pps, 2)
}

func TestListPipelines_WithLastBuildAt(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pipelines := []*pipeline.Pipeline{
		{ID: 1, Name: "pipeline-a"},
		{ID: 2, Name: "pipeline-b"},
		{ID: 3, Name: "pipeline-c"},
	}
	buildTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	lastBuilds := map[uint32]time.Time{
		1: buildTime,
		3: buildTime.Add(-time.Hour),
	}
	s.Pipelines.EXPECT().Filter(ctx, "main").Return(pipelines, nil)
	s.Builds.EXPECT().LastBuildAtByPipeline(ctx, "main").Return(lastBuilds, nil)

	pps, err := s.S.ListPipelines(ctx, "main")
	require.NoError(t, err)
	require.Len(t, pps, 3)

	require.NotNil(t, pps[0].LastBuildAt)
	assert.Equal(t, buildTime, *pps[0].LastBuildAt)

	assert.Nil(t, pps[1].LastBuildAt)

	require.NotNil(t, pps[2].LastBuildAt)
	assert.Equal(t, buildTime.Add(-time.Hour), *pps[2].LastBuildAt)
}

func TestListPipelines_LastBuildAtError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().Filter(ctx, "main").Return([]*pipeline.Pipeline{{ID: 1}}, nil)
	s.Builds.EXPECT().LastBuildAtByPipeline(ctx, "main").Return(nil, fmt.Errorf("db error"))

	_, err := s.S.ListPipelines(ctx, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "last build timestamps")
}

func TestListPipelines_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.ListPipelines(ctx, "INVALID")
	require.Error(t, err)
}

func TestUpdatePipeline_Rename(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	oldCan := "old-name"
	newName := "New Name"
	newCan := "new-name"

	b, err := os.ReadFile("testdata/pipeline.hcl")
	require.NoError(t, err)

	mvars := map[string]interface{}{
		"repo_name": "repo",
	}

	existing := &pipeline.Pipeline{
		ID: 1, Name: "old-name", Canonical: oldCan,
		Jobs:      []job.Job{{ID: 1, Name: "gen"}, {ID: 2, Name: "test"}, {ID: 3, Name: "build"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "git.qid", Name: "qid", Type: "git", Params: &resource.Params{Params: map[string]string{"url": "u", "name": "repo"}}}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, oldCan).Return(existing, nil)
	s.Pipelines.EXPECT().Update(ctx, tc, oldCan, gomock.Any()).Return(nil)
	// After update, Find uses new canonical (called twice: once to get dbpp, once at end of UoW)
	renamed := &pipeline.Pipeline{
		ID: 1, Name: newName, Canonical: newCan,
		Jobs:      existing.Jobs,
		Resources: existing.Resources,
	}
	s.Pipelines.EXPECT().Find(ctx, tc, newCan).Return(renamed, nil).Times(2)
	// Jobs: update existing (3 jobs match)
	s.Jobs.EXPECT().Update(ctx, tc, newCan, "gen", gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, newCan, "test", gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, newCan, "build", gomock.Any()).Return(nil)
	// Resources: update existing (1 resource match)
	s.Resources.EXPECT().Update(ctx, tc, newCan, "git.qid", gomock.Any()).Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, oldCan, b, mvars, newName)
	require.NoError(t, err)
	assert.Equal(t, newName, pp.Name)
	assert.Equal(t, newCan, pp.Canonical)
}

func TestUpdatePipeline_PreserveName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	b, err := os.ReadFile("testdata/pipeline.hcl")
	require.NoError(t, err)

	mvars := map[string]interface{}{
		"repo_name": "repo",
	}

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:      []job.Job{{ID: 1, Name: "gen"}, {ID: 2, Name: "test"}, {ID: 3, Name: "build"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "git.qid", Name: "qid", Type: "git", Params: &resource.Params{Params: map[string]string{"url": "u", "name": "repo"}}}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "gen", gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "build", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "git.qid", gomock.Any()).Return(nil)

	// No newName — should preserve existing name
	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, b, mvars)
	require.NoError(t, err)
	assert.Equal(t, "My Pipeline", pp.Name)
	assert.Equal(t, pCan, pp.Canonical)
}

func TestDeletePipeline(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().Delete(ctx, "main", "my-pipeline").Return(nil)

	err := s.S.DeletePipeline(ctx, "main", "my-pipeline")
	require.NoError(t, err)
}

func TestDeletePipeline_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.DeletePipeline(ctx, "INVALID", "my-pipeline")
	require.Error(t, err)

	err = s.S.DeletePipeline(ctx, "main", "INVALID")
	require.Error(t, err)
}

func TestCreatePipeline_OrderedPlanWithPut(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "git" {
  params = ["url"]
  check "exec" {
    path = "echo"
    args = ["check"]
  }
  pull "exec" {
    path = "echo"
    args = ["pull"]
  }
  push "exec" {
    path = "echo"
    args = ["push"]
  }
}

resource "git" "repo" {
  params {
    url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
  put "git" "repo" {
    tag = "latest"
  }
}
`)

	// Expect all the create calls
	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			// Verify the plan is ordered: get, task, put
			require.Len(t, j.Plan, 3)
			assert.Equal(t, job.StepTypeGet, j.Plan[0].Type)
			assert.Equal(t, "timer", j.Plan[0].Get.Name)
			assert.Equal(t, job.StepTypeTask, j.Plan[1].Type)
			assert.Equal(t, "build", j.Plan[1].Task.Name)
			assert.Equal(t, job.StepTypePut, j.Plan[2].Type)
			assert.Equal(t, "repo", j.Plan[2].Put.Name)
			assert.Equal(t, "latest", j.Plan[2].Put.Params["tag"])
			return uint32(1), nil
		})
	s.ResourceTypes.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.Pipelines.EXPECT().Find(ctx, "main", "test-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_BackwardsCompat_GetThenTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "compat-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			// Verify backwards compat: get before task
			require.Len(t, j.Plan, 2)
			assert.Equal(t, job.StepTypeGet, j.Plan[0].Type)
			assert.Equal(t, job.StepTypeTask, j.Plan[1].Type)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "compat-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "compat-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "compat-pipeline", Canonical: "compat-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "compat-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_HCLFunctions(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
variable "greeting" {
  type    = string
  default = "hello"
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = [upper(var.greeting), join(",", ["a", "b", "c"])]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "func-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 2)
			assert.Equal(t, job.StepTypeTask, j.Plan[1].Type)
			require.Len(t, j.Plan[1].Task.Run.Args, 2)
			assert.Equal(t, "HELLO", j.Plan[1].Task.Run.Args[0])
			assert.Equal(t, "a,b,c", j.Plan[1].Task.Run.Args[1])
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "func-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "func-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "func-pipeline", Canonical: "func-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "func-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_SourceAndInlineConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "my-git" {
  source = "pikoci://git"
  params = ["url"]
  check "exec" {
    path = "echo"
    args = ["check"]
  }
  pull "exec" { }
  push "exec" { }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	_, err := s.S.CreatePipeline(ctx, "main", "conflict-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both source and inline commands")
}

func TestCreatePipeline_WithTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
    timeout = "2m"
  }
  task "build" {
    timeout = "10m"
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "timeout-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 2)
			assert.Equal(t, 2*time.Minute, j.Plan[0].Timeout)
			assert.Equal(t, 10*time.Minute, j.Plan[1].Timeout)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "timeout-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "timeout-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "timeout-pipeline", Canonical: "timeout-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "timeout-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_InvalidTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    timeout = "invalid"
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "invalid-timeout-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout")
}

func TestCreatePipeline_WithJobTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  timeout = "30m"
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "job-timeout-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			assert.Equal(t, 30*time.Minute, j.Timeout)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "job-timeout-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "job-timeout-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "job-timeout-pipeline", Canonical: "job-timeout-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "job-timeout-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_InvalidJobTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  timeout = "bad"
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "invalid-job-timeout-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout")
}

func TestCreatePipeline_WithAttempts(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger  = true
    attempts = 3
  }
  task "build" {
    attempts = 2
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "attempts-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 2)
			assert.Equal(t, 3, j.Plan[0].Attempts)
			assert.Equal(t, 2, j.Plan[1].Attempts)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "attempts-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "attempts-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "attempts-pipeline", Canonical: "attempts-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "attempts-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_WithInputsOutputs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    inputs  = ["src/"]
    outputs = ["bin/app"]
    run "exec" {
      path = "make"
      args = ["build"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "inputs-outputs-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 2)
			assert.Equal(t, []string{"src/"}, j.Plan[1].Task.Inputs)
			assert.Equal(t, []string{"bin/app"}, j.Plan[1].Task.Outputs)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "inputs-outputs-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "inputs-outputs-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "inputs-outputs-pipeline", Canonical: "inputs-outputs-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "inputs-outputs-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_WithoutInputsOutputs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "no-io-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 2)
			assert.Nil(t, j.Plan[1].Task.Inputs)
			assert.Nil(t, j.Plan[1].Task.Outputs)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "no-io-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "no-io-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "no-io-pipeline", Canonical: "no-io-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "no-io-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_InvalidAttempts(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  task "build" {
    attempts = -1
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "invalid-attempts-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid attempts")
}

func TestCreatePipeline_SourceResolution(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "my-git" {
  source = "pikoci://git"
}

resource "my-git" "repo" {
  params {
    url  = "https://example.com/repo.git"
    name = "repo"
  }
}

job "test" {
  get "my-git" "repo" { trigger = true }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "source-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "source-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, rt interface{}) (uint32, error) {
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "source-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "source-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "source-pipeline", Canonical: "source-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "source-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_WithSecretType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
secret_type "vault" {
  params = ["path"]
  address = "http://vault:8200"
  token   = "my-token"
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '{\"username\":\"admin\"}'"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "migrate" {
    run "exec" {
      path = "make"
      args = ["migrate"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "secrets-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 2)
			assert.Equal(t, job.StepTypeTask, j.Plan[1].Type)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "secrets-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.SecretTypes.EXPECT().Create(ctx, "main", "secrets-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, st sectype.SecretType) (uint32, error) {
			assert.Equal(t, "vault", st.Name)
			assert.Equal(t, []string{"path"}, st.Params)
			assert.Equal(t, "http://vault:8200", st.Config["address"])
			assert.Equal(t, "my-token", st.Config["token"])
			assert.Equal(t, "exec", st.Get.Runner)
			return uint32(1), nil
		})
	s.Pipelines.EXPECT().Find(ctx, "main", "secrets-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "secrets-pipeline", Canonical: "secrets-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "secrets-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_SecretBackedVariable(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
secret_type "vault" {
  params = ["path"]
  address = "http://vault:8200"
  token   = "my-token"
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '{\"token\":\"s3cret\"}'"]
  }
}

variable "git_token" {
  type = string
  secret "vault" {
    path = "secret/data/github"
    key  = "token"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
  params {
    token = var.git_token
  }
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc string, pp pipeline.Pipeline) (uint32, error) {
			// Verify secret vars are stored on the pipeline
			require.Len(t, pp.SecretVars, 1)
			sv, ok := pp.SecretVars["git_token"]
			require.True(t, ok)
			assert.Equal(t, "vault", sv.Type)
			assert.Equal(t, "secret/data/github", sv.Path)
			assert.Equal(t, "token", sv.Key)

			// Verify the resource param contains a placeholder
			require.Len(t, pp.Resources, 1)
			tokenParam := pp.Resources[0].GetParams()["token"]
			assert.Contains(t, tokenParam, "__pikoci_secret:vault:secret/data/github:token__")
			return uint32(1), nil
		})
	s.Jobs.EXPECT().Create(ctx, "main", "secret-var-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "secret-var-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.SecretTypes.EXPECT().Create(ctx, "main", "secret-var-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "secret-var-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "secret-var-pipeline", Canonical: "secret-var-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "secret-var-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_SecretBackedVariable_VarsOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
secret_type "vault" {
  params = ["path"]
  address = "http://vault:8200"
  token   = "my-token"
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '{\"token\":\"s3cret\"}'"]
  }
}

variable "git_token" {
  type = string
  secret "vault" {
    path = "secret/data/github"
    key  = "token"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
  params {
    token = var.git_token
  }
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	vars := map[string]interface{}{"git_token": "override-token"}

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc string, pp pipeline.Pipeline) (uint32, error) {
			// When vars override is provided, secret vars should be empty
			assert.Empty(t, pp.SecretVars)

			// The resource param should have the literal override value, not a placeholder
			require.Len(t, pp.Resources, 1)
			tokenParam := pp.Resources[0].GetParams()["token"]
			assert.Equal(t, "override-token", tokenParam)
			return uint32(1), nil
		})
	s.Jobs.EXPECT().Create(ctx, "main", "override-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "override-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.SecretTypes.EXPECT().Create(ctx, "main", "override-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "override-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "override-pipeline", Canonical: "override-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "override-pipeline", hclConfig, vars)
	require.NoError(t, err)
}

func TestCreatePipeline_SecretTypeSourceAndInlineConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
secret_type "vault" {
  source = "pikoci://vault"
  params = ["path"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo test"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	_, err := s.S.CreatePipeline(ctx, "main", "conflict-secret-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both source and inline commands")
}

func TestCreatePipeline_WithServices(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
service_type "test-db" {
  params = ["version"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo starting"]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "echo ready"]
    interval = "1s"
    timeout  = "10s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo stopping"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  service "test-db" {
    version = "15"
  }

  get "cron" "timer" {
    trigger = true
  }
  task "run-tests" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "services-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 3) // service + get + task
			assert.Equal(t, job.StepTypeService, j.Plan[0].Type)
			assert.Equal(t, "test-db", j.Plan[0].Service.Name)
			assert.Equal(t, map[string]string{"version": "15"}, j.Plan[0].Service.Params)
			assert.Equal(t, job.StepTypeGet, j.Plan[1].Type)
			assert.Equal(t, job.StepTypeTask, j.Plan[2].Type)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "services-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "services-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "services-pipeline", Canonical: "services-pipeline"}, nil)

	pp, err := s.S.CreatePipeline(ctx, "main", "services-pipeline", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestCreatePipeline_ServiceNoInlineAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Inline service definitions inside jobs are not supported.
	// Services must be defined at the top level.
	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  service "inline-db" {}

  get "cron" "timer" {
    trigger = true
  }
  task "run-tests" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "no-inline-svc-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service_type \"inline-db\" referenced in job")
}

func TestCreatePipeline_ServiceMissingReference(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  service "nonexistent" {}

  get "cron" "timer" {
    trigger = true
  }
  task "run-tests" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "svc-missing-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service_type \"nonexistent\" referenced in job")
}

func TestCreatePipeline_ServiceMissingStart(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
service_type "bad" {
  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo stopping"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  service "bad" {}
  get "cron" "timer" { trigger = true }
  task "run-tests" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "svc-no-start-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must have a start block")
}

func TestCreatePipeline_ServiceSourceAndInlineConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
service_type "bad" {
  source = "https://example.com/service.hcl"
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo starting"]
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo stopping"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  service "bad" {}
  get "cron" "timer" { trigger = true }
  task "run-tests" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "svc-source-conflict-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both source and inline commands")
}

func TestCreatePipeline_HooksLabeledRunner(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" { trigger = true }
  task "run" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
    on_success "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo success"]
    }
    on_failure "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo failure"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "hooks-labeled", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "hooks-labeled", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "hooks-labeled").Return(&pipeline.Pipeline{Name: "hooks-labeled", Canonical: "hooks-labeled"}, nil)

	pp, err := s.S.CreatePipeline(ctx, "main", "hooks-labeled", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestCreatePipeline_HooksUnlabeledPut(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "notify" {
  push "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

resource "notify" "slack" {
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" { trigger = true }
  task "run" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
    on_success {
      put "notify" "slack" {
        message = "success"
      }
    }
    on_failure {
      put "notify" "slack" {
        message = "failure"
      }
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "hooks-unlabeled", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "hooks-unlabeled", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "hooks-unlabeled", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "hooks-unlabeled").Return(&pipeline.Pipeline{Name: "hooks-unlabeled", Canonical: "hooks-unlabeled"}, nil)

	pp, err := s.S.CreatePipeline(ctx, "main", "hooks-unlabeled", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestCreatePipeline_HooksMixed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Mix labeled runner hooks and unlabeled put hooks in the same job
	hclConfig := []byte(`
resource_type "notify" {
  push "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

resource "notify" "slack" {
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" { trigger = true }

  task "run" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }

  on_success "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo job-level-success"]
  }

  on_success {
    put "notify" "slack" {
      message = "success"
    }
  }

  on_failure {
    put "notify" "slack" {
      message = "failure"
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "hooks-mixed", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "hooks-mixed", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "hooks-mixed", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "hooks-mixed").Return(&pipeline.Pipeline{Name: "hooks-mixed", Canonical: "hooks-mixed"}, nil)

	pp, err := s.S.CreatePipeline(ctx, "main", "hooks-mixed", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestCreatePipeline_HooksOnPutStep(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "notify" {
  push "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

resource "notify" "slack" {
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" { trigger = true }

  task "run" {
    run "exec" {
      path = "echo"
      args = ["testing"]
    }
  }

  put "notify" "slack" {
    message = "deployed"

    on_success "exec" {
      path = "/bin/sh"
      args = ["-ec", "echo put-success"]
    }

    on_failure {
      put "notify" "slack" {
        message = "put-failed"
      }
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "hooks-on-put", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "hooks-on-put", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "hooks-on-put", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "hooks-on-put").Return(&pipeline.Pipeline{Name: "hooks-on-put", Canonical: "hooks-on-put"}, nil)

	pp, err := s.S.CreatePipeline(ctx, "main", "hooks-on-put", hclConfig, nil)
	require.NoError(t, err)
	require.NotNil(t, pp)
}

func TestGetPipelineImage_HidesUnlinkedResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Pipeline with two resources: one used in a get step, one only in a hook put step.
	pp := &pipeline.Pipeline{
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer"},
			{ID: 2, Canonical: "github-check.ci"},
		},
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "build",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "timer", Trigger: true},
					},
					{
						Type: job.StepTypeTask,
					},
				},
				OnSuccess: []job.HookStep{
					{
						Type: job.StepTypePut,
						Put:  &job.PutStep{Type: "github-check", Name: "ci"},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "build", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{}, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)

	dot := string(img)
	// The get-step resource should appear as a primary node
	assert.True(t, strings.Contains(dot, `"cron.timer"`), "linked resource should appear in graph")
	// The hook-only resource should NOT appear as a primary node (only as a put output node)
	// A primary node would be just "github-check.ci"; the put output node is "build-github-check.ci-out"
	lines := strings.Split(dot, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip edges and put output nodes
		if strings.Contains(trimmed, "->") || strings.Contains(trimmed, "-out") {
			continue
		}
		if strings.Contains(trimmed, `"github-check.ci"`) && strings.Contains(trimmed, "shape") {
			t.Errorf("unlinked resource should not appear as a primary node in graph, got: %s", trimmed)
		}
	}
}

func TestGetPipelineImage_QuotesHyphenatedName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Name:      "hello-world",
		Canonical: "hello-world",
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.tick"},
		},
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "hello",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "tick", Trigger: true},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "hello-world").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "hello-world", "hello", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{}, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "hello-world", "dot")
	require.NoError(t, err)

	dot := string(img)
	// Pipeline name must be quoted in DOT output so hyphens aren't parsed as minus operator
	assert.True(t, strings.Contains(dot, `"hello-world"`), "hyphenated pipeline name should be quoted in DOT output")
	assert.False(t, strings.HasPrefix(strings.TrimSpace(dot), "strict graph hello-world"), "unquoted hyphenated name should not appear")
}

func TestGetPipelineImage_ShowsLinkedResources(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Both resources are used in get steps - both should appear.
	pp := &pipeline.Pipeline{
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer"},
			{ID: 2, Canonical: "git.repo"},
		},
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "test",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "timer", Trigger: true},
					},
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "git", Name: "repo"},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "test", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{}, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)

	dot := string(img)
	assert.True(t, strings.Contains(dot, `"cron.timer"`), "first linked resource should appear")
	assert.True(t, strings.Contains(dot, `"git.repo"`), "second linked resource should appear")
}

func TestGetPipelineImage_PassedReusesPutOutputNode(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Name:      "artifact-pipeline",
		Canonical: "artifact-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer"},
			{ID: 2, Canonical: "artifact.output"},
		},
		Jobs: []job.Job{
			{
				ID:   1,
				Name: "build",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "cron", Name: "timer", Trigger: true},
					},
					{
						Type: job.StepTypePut,
						Put:  &job.PutStep{Type: "artifact", Name: "output"},
					},
				},
			},
			{
				ID:   2,
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get:  &job.GetStep{Type: "artifact", Name: "output", Trigger: true, Passed: []string{"build"}},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "artifact-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "artifact-pipeline", "build", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{}, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "artifact-pipeline", "deploy", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{}, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "artifact-pipeline", "dot")
	require.NoError(t, err)

	dot := string(img)
	// The put output node should exist
	assert.Contains(t, dot, `"build-artifact.output-out"`, "put output node should exist")
	// The passed edge should reuse the put output node (edge from put-out to deploy)
	assert.Contains(t, dot, `"build-artifact.output-out"--"deploy"`, "passed edge should reuse put output node")
	// There should NOT be a separate passed node for the same resource
	assert.NotContains(t, dot, `"build-output-deploy"`, "should not create a separate passed node when put output exists")
	// The resource should NOT appear as an orphaned standalone node since it's
	// only accessed via get-with-passed (the put output node handles it)
	lines := strings.Split(dot, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "->") || strings.Contains(trimmed, "-out") {
			continue
		}
		if strings.Contains(trimmed, `"artifact.output"`) && strings.Contains(trimmed, "shape") {
			t.Errorf("resource only accessed via get-with-passed should not appear as standalone node: %s", trimmed)
		}
	}
}

func TestGetPipelineImage_JobStatusColors(t *testing.T) {
	// Helper to build a simple pipeline with one job triggered by a cron resource.
	makePipeline := func() *pipeline.Pipeline {
		return &pipeline.Pipeline{
			Name:      "p",
			Canonical: "p",
			Resources: []resource.Resource{
				{ID: 1, Canonical: "cron.tick"},
			},
			Jobs: []job.Job{
				{
					ID:   1,
					Name: "j",
					Plan: []job.PlanStep{
						{
							Type: job.StepTypeGet,
							Get:  &job.GetStep{Type: "cron", Name: "tick", Trigger: true},
						},
					},
				},
			},
		}
	}

	// Color constants matching pipelines.go
	colorSucceeded := `"#00A83A"`
	colorFailed := `"#FF004D"`
	colorDefault := `"#83769C"`
	colorStartedBorder := `"#CC8200"`
	colorDefaultBorder := `"#5F574F"`

	tests := []struct {
		name            string
		builds          []*build.Build
		wantFillColor   string
		wantDashedStyle bool
		wantBorderColor string
	}{
		{
			name:            "no builds - default color, no outline",
			builds:          []*build.Build{},
			wantFillColor:   colorDefault,
			wantDashedStyle: false,
		},
		{
			name: "latest main build succeeded",
			builds: []*build.Build{
				{ID: 2, BuildNumber: "2", Status: build.Succeeded},
				{ID: 1, BuildNumber: "1", Status: build.Failed},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: false,
		},
		{
			name: "latest main build failed",
			builds: []*build.Build{
				{ID: 2, BuildNumber: "2", Status: build.Failed},
				{ID: 1, BuildNumber: "1", Status: build.Succeeded},
			},
			wantFillColor:   colorFailed,
			wantDashedStyle: false,
		},
		{
			name: "latest main build running - shows previous color with dashed outline",
			builds: []*build.Build{
				{ID: 2, BuildNumber: "2", Status: build.Started},
				{ID: 1, BuildNumber: "1", Status: build.Succeeded},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
		{
			name: "retry running - latest main build color with dashed outline",
			builds: []*build.Build{
				{ID: 2, BuildNumber: "1.1", Status: build.Started},
				{ID: 1, BuildNumber: "1", Status: build.Failed},
			},
			wantFillColor:   colorFailed,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
		{
			name: "retry succeeded - color reflects retry success",
			builds: []*build.Build{
				{ID: 2, BuildNumber: "1.1", Status: build.Succeeded},
				{ID: 1, BuildNumber: "1", Status: build.Failed},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: false,
		},
		{
			name: "only build is running - default color with dashed outline",
			builds: []*build.Build{
				{ID: 1, BuildNumber: "1", Status: build.Started},
			},
			wantFillColor:   colorDefault,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
		{
			name: "multiple main builds with retries - latest main build wins",
			builds: []*build.Build{
				{ID: 4, BuildNumber: "2.1", Status: build.Started},
				{ID: 3, BuildNumber: "2", Status: build.Failed},
				{ID: 2, BuildNumber: "1.1", Status: build.Succeeded},
				{ID: 1, BuildNumber: "1", Status: build.Succeeded},
			},
			wantFillColor:   colorFailed,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
		{
			name: "new build running - fallback uses retry from previous group",
			builds: []*build.Build{
				{ID: 3, BuildNumber: "2", Status: build.Started},
				{ID: 2, BuildNumber: "1.1", Status: build.Succeeded},
				{ID: 1, BuildNumber: "1", Status: build.Cancelled},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
		{
			name: "pending build with previous success - shows previous color with gray dashed outline",
			builds: []*build.Build{
				{ID: 2, BuildNumber: "2", Status: build.Pending},
				{ID: 1, BuildNumber: "1", Status: build.Succeeded},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: true,
			wantBorderColor: colorDefaultBorder,
		},
		{
			name: "pending and running builds - running takes priority with orange outline",
			builds: []*build.Build{
				{ID: 3, BuildNumber: "1.1", Status: build.Started},
				{ID: 2, BuildNumber: "2", Status: build.Pending},
				{ID: 1, BuildNumber: "1", Status: build.Succeeded},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
		{
			name: "only build is pending - default color with gray dashed outline",
			builds: []*build.Build{
				{ID: 1, BuildNumber: "1", Status: build.Pending},
			},
			wantFillColor:   colorDefault,
			wantDashedStyle: true,
			wantBorderColor: colorDefaultBorder,
		},
		{
			name: "pending + running + succeeded across 3 main builds - shows succeeded color",
			builds: []*build.Build{
				{ID: 3, BuildNumber: "3", Status: build.Pending},
				{ID: 2, BuildNumber: "2", Status: build.Started},
				{ID: 1, BuildNumber: "1", Status: build.Succeeded},
			},
			wantFillColor:   colorSucceeded,
			wantDashedStyle: true,
			wantBorderColor: colorStartedBorder,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			s := newService(ctrl)
			ctx := context.TODO()

			pp := makePipeline()
			s.Pipelines.EXPECT().Find(ctx, "main", "p").Return(pp, nil)
			s.Builds.EXPECT().Filter(ctx, "main", "p", "j", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return(tt.builds, nil)

			img, err := s.S.GetPipelineImage(ctx, "main", "p", "dot")
			require.NoError(t, err)

			dot := string(img)

			// Check fill color on the job node
			assert.Contains(t, dot, fmt.Sprintf("fillcolor=%s", tt.wantFillColor),
				"expected fill color %s", tt.wantFillColor)

			// Check dashed outline on the subgraph cluster
			if tt.wantDashedStyle {
				assert.Contains(t, dot, fmt.Sprintf("color=%s", tt.wantBorderColor),
					"expected border color %s on subgraph", tt.wantBorderColor)
				assert.Contains(t, dot, `style="dashed,bold"`,
					"expected dashed style on subgraph when build is running")
			} else {
				assert.Contains(t, dot, "style=invis",
					"expected invisible style on subgraph when no build is running")
			}
		})
	}
}

func TestCreatePipeline_WithNotificationType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "slack" {
  params = ["webhook_url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

notification "slack" "deploys" {
  params {
    webhook_url = "https://hooks.slack.com/test"
  }
  message = "Deploy complete"
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "build" {
  get "cron" "timer" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "test-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_NotifyInPlan(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "slack" {
  params = ["webhook_url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

notification "slack" "deploys" {
  params {
    webhook_url = "https://hooks.slack.com/test"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  notify "slack" "deploys" {
    status = "in_progress"
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 3)
			assert.Equal(t, job.StepTypeGet, j.Plan[0].Type)
			assert.Equal(t, job.StepTypeNotify, j.Plan[1].Type)
			assert.Equal(t, "deploys", j.Plan[1].Notify.Name)
			assert.Equal(t, "slack", j.Plan[1].Notify.Type)
			assert.Equal(t, "in_progress", j.Plan[1].Notify.Params["status"])
			assert.Equal(t, job.StepTypeTask, j.Plan[2].Type)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "test-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_NotifyInHooks(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "slack" {
  params = ["webhook_url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

notification "slack" "deploys" {
  params {
    webhook_url = "https://hooks.slack.com/test"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  get "cron" "timer" {
    trigger = true
  }
  task "build" {
    run "exec" {
      path = "echo"
      args = ["building"]
    }
  }
  on_success {
    notify "slack" "deploys" { conclusion = "success" }
  }
  on_failure {
    notify "slack" "deploys" { conclusion = "failure" }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.OnSuccess, 1)
			assert.Equal(t, job.StepTypeNotify, j.OnSuccess[0].Type)
			assert.Equal(t, "deploys", j.OnSuccess[0].Notify.Name)
			assert.Equal(t, "success", j.OnSuccess[0].Notify.Params["conclusion"])

			require.Len(t, j.OnFailure, 1)
			assert.Equal(t, job.StepTypeNotify, j.OnFailure[0].Type)
			assert.Equal(t, "deploys", j.OnFailure[0].Notify.Name)
			assert.Equal(t, "failure", j.OnFailure[0].Notify.Params["conclusion"])
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "test-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_NotificationValidation(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	tests := []struct {
		name    string
		hcl     string
		wantErr string
	}{
		{
			name: "jobs and exclude mutual exclusion",
			hcl: `
notification_type "slack" {
  notify "exec" {
    path = "echo"
  }
}
notification "slack" "n" {
  on = ["success"]
  jobs = ["a"]
  exclude = ["b"]
}
job "a" {
  task "t" {
    run "exec" {
      path = "echo"
    }
  }
}
`,
			wantErr: "jobs and exclude are mutually exclusive",
		},
		{
			name: "jobs requires on",
			hcl: `
notification_type "slack" {
  notify "exec" {
    path = "echo"
  }
}
notification "slack" "n" {
  jobs = ["a"]
}
job "a" {
  task "t" {
    run "exec" {
      path = "echo"
    }
  }
}
`,
			wantErr: "jobs/exclude requires on field",
		},
		{
			name: "invalid on event",
			hcl: `
notification_type "slack" {
  notify "exec" {
    path = "echo"
  }
}
notification "slack" "n" {
  on = ["invalid"]
}
job "a" {
  task "t" {
    run "exec" {
      path = "echo"
    }
  }
}
`,
			wantErr: "invalid on event",
		},
		{
			name: "all cannot combine",
			hcl: `
notification_type "slack" {
  notify "exec" {
    path = "echo"
  }
}
notification "slack" "n" {
  on = ["all", "success"]
}
job "a" {
  task "t" {
    run "exec" {
      path = "echo"
    }
  }
}
`,
			wantErr: "'all' cannot be combined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", []byte(tt.hcl), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCreatePipeline_NotificationInlineType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "github-check" {
  params = ["app_id", "repository"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo check"]
  }
}

notification "github-check" "ci" {
  params {
    app_id     = "123"
    repository = "test/repo"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "build" {
  get "cron" "timer" {
    trigger = true
  }
  notify "github-check" "ci" {
    status = "in_progress"
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			require.Len(t, j.Plan, 3)
			assert.Equal(t, job.StepTypeNotify, j.Plan[1].Type)
			assert.Equal(t, "github-check", j.Plan[1].Notify.Type)
			assert.Equal(t, "ci", j.Plan[1].Notify.Name)
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "test-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_NotificationAutoOn(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "slack" {
  params = ["webhook_url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notifying"]
  }
}

notification "slack" "deploys" {
  params {
    webhook_url = "https://hooks.slack.com/test"
  }
  on = ["success", "failure"]
  message = "Build finished"
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "build" {
  get "cron" "timer" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "test-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "test-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "test-pipeline", Canonical: "test-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "test-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_RunnerOverride_ResourceType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "git" {
  params = ["url"]
  check "exec" {
    path = "/opt/git/check"
    args = ["-v"]
  }
  pull "exec" {
    path = "/opt/git/pull"
  }
  push "exec" {
    path = "/opt/git/push"
  }
  runner "docker" {
    image = "alpine/git:latest"
  }
}

resource "git" "repo" {
  params {
    url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "runner-override", gomock.Any()).Return(uint32(1), nil)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "runner-override", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, rt interface{}) (uint32, error) {
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "runner-override", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.Pipelines.EXPECT().Find(ctx, "main", "runner-override").Return(&pipeline.Pipeline{ID: 1, Name: "runner-override", Canonical: "runner-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "runner-override", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_RunnerOverride_RejectsNonExec(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "custom" {
  check "docker" {
    image = "foo"
    cmd   = "check"
  }
  pull "docker" {
    image = "foo"
    cmd   = "pull"
  }
  runner "docker" {
    image = "alpine/git:latest"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "reject-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner override but command uses non-exec runner")
}

func TestCreatePipeline_RunnerOverride_NotificationType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "slack" {
  source = "pikoci://slack"
  runner "docker" {
    image = "curlimages/curl:latest"
  }
}

notification "slack" "alerts" {
  params {
    webhook_url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "notif-override", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "notif-override", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "notif-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "notif-override", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "notif-override").Return(&pipeline.Pipeline{ID: 1, Name: "notif-override", Canonical: "notif-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "notif-override", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_RunnerOverride_SecretType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
secret_type "vault" {
  source = "pikoci://vault"
  runner "docker" {
    image = "hashicorp/vault:latest"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "secret-override", gomock.Any()).Return(uint32(1), nil)
	s.SecretTypes.EXPECT().Create(ctx, "main", "secret-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "secret-override", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "secret-override").Return(&pipeline.Pipeline{ID: 1, Name: "secret-override", Canonical: "secret-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "secret-override", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_RunnerOverride_ServiceType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
service_type "postgresql" {
  source = "pikoci://postgresql"
  runner "docker" {
    image = "docker:latest"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  service "postgresql" {}
  get "cron" "timer" {
    trigger = true
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "svc-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "svc-override", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "svc-override").Return(&pipeline.Pipeline{ID: 1, Name: "svc-override", Canonical: "svc-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "svc-override", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_RunnerOverride_RejectsNonExec_NotificationType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "custom" {
  notify "docker" {
    image = "foo"
    cmd   = "send"
  }
  runner "docker" {
    image = "bar:latest"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "reject-notif", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notification_type")
	assert.Contains(t, err.Error(), "runner override but command uses non-exec runner")
}

func TestCreatePipeline_RunnerOverride_RejectsNonExec_SecretType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
secret_type "custom" {
  get "docker" {
    image = "foo"
    cmd   = "get-secret"
  }
  runner "docker" {
    image = "bar:latest"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "reject-secret", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret_type")
	assert.Contains(t, err.Error(), "runner override but command uses non-exec runner")
}

func TestCreatePipeline_RunnerOverride_RejectsNonExec_ServiceType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
service_type "svc" {
  start "docker" {
    image = "foo"
    cmd   = "start"
  }
  stop "exec" {
    path = "echo"
    args = ["stop"]
  }
  runner "docker" {
    image = "bar:latest"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "reject-svc", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service_type")
	assert.Contains(t, err.Error(), "runner override but command uses non-exec runner")
}

func TestCreatePipeline_RunnerOverride_ResourceTypeWithArgs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "git" {
  params = ["url"]
  check "exec" {
    path = "/opt/git/check"
  }
  pull "exec" {
    path = "/opt/git/pull"
  }
  push "exec" {
    path = "/opt/git/push"
  }
  runner "docker" {
    image = "alpine/git:latest"
    args  = ["-v", "/var/run/docker.sock:/var/run/docker.sock"]
  }
}

resource "git" "repo" {
  params {
    url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "args-override", gomock.Any()).Return(uint32(1), nil)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "args-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "args-override", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.Pipelines.EXPECT().Find(ctx, "main", "args-override").Return(&pipeline.Pipeline{ID: 1, Name: "args-override", Canonical: "args-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "args-override", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_RunnerOverride_SourcedResourceType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "git" {
  source = "pikoci://git"
  runner "docker" {
    image = "alpine/git:latest"
  }
}

resource "git" "repo" {
  params {
    url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "sourced-override", gomock.Any()).Return(uint32(1), nil)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "sourced-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "sourced-override", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.Pipelines.EXPECT().Find(ctx, "main", "sourced-override").Return(&pipeline.Pipeline{ID: 1, Name: "sourced-override", Canonical: "sourced-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "sourced-override", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_CacheOnResourceType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "git" {
  cache  = true
  params = ["url"]
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '[{\"ref\":\"abc\"}]'"]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo pull"]
  }
  push "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo push"]
  }
}

resource "git" "repo" {
  params {
    url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	var capturedRT interface{}
	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "cache-test", gomock.Any()).Return(uint32(1), nil)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "cache-test", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, rt interface{}) (uint32, error) {
			capturedRT = rt
			return uint32(1), nil
		})
	s.Resources.EXPECT().Create(ctx, "main", "cache-test", gomock.Any()).Return(uint32(1), nil).Times(2)
	s.Pipelines.EXPECT().Find(ctx, "main", "cache-test").Return(&pipeline.Pipeline{ID: 1, Name: "cache-test", Canonical: "cache-test"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "cache-test", hclConfig, nil)
	require.NoError(t, err)

	rt, ok := capturedRT.(restype.ResourceType)
	require.True(t, ok)
	assert.True(t, rt.Cache)
}

func TestCreatePipeline_CacheResourceOverridesType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource_type "git" {
  cache  = true
  params = ["url"]
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo '[{\"ref\":\"abc\"}]'"]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo pull"]
  }
  push "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo push"]
  }
}

resource "git" "repo" {
  cache = false
  params {
    url = "http://example.com"
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "test" {
  get "cron" "timer" {
    trigger = true
  }
}
`)

	var capturedResource resource.Resource
	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "cache-override", gomock.Any()).Return(uint32(1), nil)
	s.ResourceTypes.EXPECT().Create(ctx, "main", "cache-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "cache-override", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, r resource.Resource) (uint32, error) {
			if r.Name == "repo" {
				capturedResource = r
			}
			return uint32(1), nil
		}).Times(2)
	s.Pipelines.EXPECT().Find(ctx, "main", "cache-override").Return(&pipeline.Pipeline{ID: 1, Name: "cache-override", Canonical: "cache-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "cache-override", hclConfig, nil)
	require.NoError(t, err)

	require.NotNil(t, capturedResource.Cache)
	assert.False(t, *capturedResource.Cache)
}

func TestSetPipelinePublic(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().SetPublic(ctx, "main", "my-pipeline", true).Return(nil)

	err := s.S.SetPipelinePublic(ctx, "main", "my-pipeline", true)
	require.NoError(t, err)
}

func TestSetPipelinePublic_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.SetPipelinePublic(ctx, "INVALID", "my-pipeline", true)
	require.Error(t, err)

	err = s.S.SetPipelinePublic(ctx, "main", "INVALID", true)
	require.Error(t, err)
}

func TestGetPublicPipeline(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Resources: []resource.Resource{
			{ID: 1, Canonical: "git.repo", Params: &resource.Params{}, WebhookToken: "secret-token", Logs: "some logs"},
		},
	}, nil)

	pp, err := s.S.GetPublicPipeline(ctx, "main", "my-pipeline")
	require.NoError(t, err)
	assert.Equal(t, "my-pipeline", pp.Canonical)
	// Sensitive fields should be sanitized
	assert.Nil(t, pp.Resources[0].Params)
	assert.Empty(t, pp.Resources[0].WebhookToken)
	assert.Empty(t, pp.Resources[0].Logs)
}

func TestGetPublicPipeline_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, err := s.S.GetPublicPipeline(ctx, "main", "my-pipeline")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found or not public")
}

func TestGetPublicPipelineJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		ID: 1, Name: "my-pipeline", Canonical: "my-pipeline",
	}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{ID: 1, Name: "my-job"}, nil)

	j, err := s.S.GetPublicPipelineJob(ctx, "main", "my-pipeline", "my-job")
	require.NoError(t, err)
	assert.Equal(t, "my-job", j.Name)
}

func TestListPublicJobBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		ID: 1, Canonical: "my-pipeline",
	}, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{
		{
			ID: 1, BuildNumber: "1", Status: build.Succeeded,
			Steps: []build.Step{
				{Type: "get", Name: "repo", Logs: "get logs"},
				{Type: "secret", Name: "my-secret", Logs: "secret value"},
			},
		},
	}, nil)

	builds, hasMore, err := s.S.ListPublicJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, nil, 0)
	require.NoError(t, err)
	assert.False(t, hasMore)
	require.Len(t, builds, 1)
	// Non-secret step logs should be preserved
	assert.Equal(t, "get logs", builds[0].Steps[0].Logs)
	// Secret step logs should be redacted
	assert.Empty(t, builds[0].Steps[1].Logs)
}

func TestGetPublicPipelineResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		ID: 1, Canonical: "my-pipeline",
	}, nil)
	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").Return(&resource.Resource{
		ID: 1, Canonical: "git.repo", Params: &resource.Params{}, WebhookToken: "secret", Logs: "logs",
	}, nil)

	r, err := s.S.GetPublicPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.NoError(t, err)
	assert.Equal(t, "git.repo", r.Canonical)
	// Sensitive fields should be sanitized
	assert.Nil(t, r.Params)
	assert.Empty(t, r.WebhookToken)
	assert.Empty(t, r.Logs)
}

func TestListPublicResourceVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		ID: 1, Canonical: "my-pipeline",
	}, nil)
	s.Resources.EXPECT().FilterVersions(ctx, "main", "my-pipeline", "git.repo", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*resource.Version{
		{ID: 2},
		{ID: 1},
	}, nil)

	vers, hasMore, err := s.S.ListPublicResourceVersions(ctx, "main", "my-pipeline", "git.repo", nil, nil, 0)
	require.NoError(t, err)
	assert.False(t, hasMore)
	require.Len(t, vers, 2)
	assert.Equal(t, uint32(2), vers[0].ID)
}

func TestUpdatePipeline_InvalidTeamCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.UpdatePipeline(ctx, "INVALID", "my-pipeline", []byte(`job "x" {}`), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")
}

func TestUpdatePipeline_InvalidPipelineCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.UpdatePipeline(ctx, "main", "INVALID", []byte(`job "x" {}`), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")
}

func TestUpdatePipeline_NewJobsCreated(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	// Pipeline config with a new job not in the existing DB
	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "existing" {
  get "cron" "timer" { trigger = true }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}

job "new-job" {
  get "cron" "timer" { trigger = true }
  task "echo2" {
    run "exec" {
      path = "echo"
      args = ["world"]
    }
  }
}
`)

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:      []job.Job{{ID: 1, Name: "existing"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	// existing job is updated
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "existing", gomock.Any()).Return(nil)
	// new-job is created
	s.Jobs.EXPECT().Create(ctx, tc, pCan, gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			assert.Equal(t, "new-job", j.Name)
			return uint32(2), nil
		})
	// Resource is updated (same check_interval, keep NextCheck)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.Equal(t, pCan, pp.Canonical)
}

func TestUpdatePipeline_DeletedJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	// Pipeline config with only one job
	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "keep" {
  get "cron" "timer" { trigger = true }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`)

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:      []job.Job{{ID: 1, Name: "keep"}, {ID: 2, Name: "remove-me"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "keep", gomock.Any()).Return(nil)
	// remove-me should be deleted
	s.Jobs.EXPECT().Delete(ctx, tc, pCan, "remove-me").Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_ResourceTypesCreateUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
resource_type "git" {
  params = ["url"]
  check "exec" {
    path = "echo"
    args = ["check"]
  }
  pull "exec" {
    path = "echo"
    args = ["pull"]
  }
  push "exec" {
    path = "echo"
    args = ["push"]
  }
}

resource_type "new-type" {
  params = ["name"]
  check "exec" {
    path = "echo"
    args = ["check"]
  }
  pull "exec" {
    path = "echo"
    args = ["pull"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:          []job.Job{{ID: 1, Name: "test"}},
		Resources:     []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
		ResourceTypes: []restype.ResourceType{{ID: 1, Name: "git"}, {ID: 2, Name: "old-type"}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)
	// git is updated, new-type is created, old-type is deleted
	s.ResourceTypes.EXPECT().Update(ctx, tc, pCan, "git", gomock.Any()).Return(nil)
	s.ResourceTypes.EXPECT().Create(ctx, tc, pCan, gomock.Any()).Return(uint32(3), nil)
	s.ResourceTypes.EXPECT().Delete(ctx, tc, pCan, "old-type").Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_ResourcesCreateUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

resource "cron" "new-res" {
  check_interval = "@every 5m"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs: []job.Job{{ID: 1, Name: "test"}},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"},
			{ID: 2, Canonical: "cron.old-res", Name: "old-res", Type: "cron"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	// timer is updated, new-res is created, old-res is deleted
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Create(ctx, tc, pCan, gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, r resource.Resource) (uint32, error) {
			assert.True(t, strings.HasPrefix(r.WebhookToken, r.Canonical+"_"),
				"webhook token should start with canonical prefix, got %q", r.WebhookToken)
			return uint32(3), nil
		},
	)
	s.Resources.EXPECT().Delete(ctx, tc, pCan, "cron.old-res").Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_ResourceChangedCheckInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 5m"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs: []job.Job{{ID: 1, Name: "test"}},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	// Resource check_interval changed, so NextCheck should be recomputed
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn, rCan string, r resource.Resource) error {
			assert.False(t, r.NextCheck.IsZero(), "NextCheck should be set when check_interval changes")
			return nil
		})

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_RunnersCreateUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
runner_type "docker" {
  run {
    path = "docker"
    args = ["run"]
  }
}

runner_type "new-runner" {
  run {
    path = "podman"
    args = ["run"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:      []job.Job{{ID: 1, Name: "test"}},
		Resources: []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
		Runners:   []runner.Runner{{ID: 1, Name: "docker"}, {ID: 2, Name: "old-runner"}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)
	// docker is updated, new-runner is created, old-runner is deleted
	s.Runners.EXPECT().Update(ctx, tc, pCan, "docker", gomock.Any()).Return(nil)
	s.Runners.EXPECT().Create(ctx, tc, pCan, gomock.Any()).Return(uint32(3), nil)
	s.Runners.EXPECT().Delete(ctx, tc, pCan, "old-runner").Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_SecretTypesCreateUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
secret_type "vault" {
  params = ["path"]
  address = "http://vault:8200"
  token   = "my-token"
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo test"]
  }
}

secret_type "new-secret" {
  params = ["key"]
  get "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo new"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:        []job.Job{{ID: 1, Name: "test"}},
		Resources:   []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
		SecretTypes: []sectype.SecretType{{ID: 1, Name: "vault"}, {ID: 2, Name: "old-secret"}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)
	// vault is updated, new-secret is created, old-secret is deleted
	s.SecretTypes.EXPECT().Update(ctx, tc, pCan, "vault", gomock.Any()).Return(nil)
	s.SecretTypes.EXPECT().Create(ctx, tc, pCan, gomock.Any()).Return(uint32(3), nil)
	s.SecretTypes.EXPECT().Delete(ctx, tc, pCan, "old-secret").Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_NotificationTypesCreateUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
notification_type "slack" {
  params = ["channel"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notify"]
  }
}

notification_type "new-nt" {
  params = ["url"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo new"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:              []job.Job{{ID: 1, Name: "test"}},
		Resources:         []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
		NotificationTypes: []notiftype.NotificationType{{ID: 1, Name: "slack"}, {ID: 2, Name: "old-nt"}},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)
	// slack is updated, new-nt is created, old-nt is deleted
	s.NotificationTypes.EXPECT().Update(ctx, tc, pCan, "slack", gomock.Any()).Return(nil)
	s.NotificationTypes.EXPECT().Create(ctx, tc, pCan, gomock.Any()).Return(uint32(3), nil)
	s.NotificationTypes.EXPECT().Delete(ctx, tc, pCan, "old-nt").Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_NotificationsCreateUpdateDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
notification_type "slack" {
  params = ["channel"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notify"]
  }
}

notification "slack" "deploy-notif" {
  on = ["success", "failure"]
}

notification "slack" "new-notif" {
  on = ["success"]
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
		Jobs:              []job.Job{{ID: 1, Name: "test"}},
		Resources:         []resource.Resource{{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", CheckInterval: "@every 1h"}},
		NotificationTypes: []notiftype.NotificationType{{ID: 1, Name: "slack"}},
		Notifications: []notification.Notification{
			{ID: 1, Type: "slack", Name: "deploy-notif", Canonical: "slack.deploy-notif"},
			{ID: 2, Type: "slack", Name: "old-notif", Canonical: "slack.old-notif"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil).Times(3)
	s.Pipelines.EXPECT().Update(ctx, tc, pCan, gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Update(ctx, tc, pCan, "test", gomock.Any()).Return(nil)
	s.Resources.EXPECT().Update(ctx, tc, pCan, "cron.timer", gomock.Any()).Return(nil)
	s.NotificationTypes.EXPECT().Update(ctx, tc, pCan, "slack", gomock.Any()).Return(nil)
	// deploy-notif is updated, new-notif is created, old-notif is deleted
	s.Notifications.EXPECT().Update(ctx, tc, pCan, "slack.deploy-notif", gomock.Any()).Return(nil)
	s.Notifications.EXPECT().Create(ctx, tc, pCan, gomock.Any()).Return(uint32(3), nil)
	s.Notifications.EXPECT().Delete(ctx, tc, pCan, "slack.old-notif").Return(nil)

	pp, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil)
	require.NoError(t, err)
	assert.NotNil(t, pp)
}

func TestUpdatePipeline_InvalidNewName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()
	tc := "main"
	pCan := "my-pipeline"

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	existing := &pipeline.Pipeline{
		ID: 1, Name: "My Pipeline", Canonical: pCan,
	}

	s.Pipelines.EXPECT().Find(ctx, tc, pCan).Return(existing, nil)

	// Empty string canonicalizes to empty which is invalid
	_, err := s.S.UpdatePipeline(ctx, tc, pCan, hclConfig, nil, "!!!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Name")
}

func TestUpdatePipeline_PipelineNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, err := s.S.UpdatePipeline(ctx, "main", "my-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find Pipeline")
}

func TestUpdatePipeline_BadConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.UpdatePipeline(ctx, "main", "my-pipeline", []byte(`this is not valid HCL !!!`), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read Pipeline config")
}

func TestSanitizePipelineForPublic_FullSanitization(t *testing.T) {
	// This test exercises sanitizePipelineForPublic with all entity types populated
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Raw:       []byte("sensitive config"),
		Resources: []resource.Resource{
			{ID: 1, Canonical: "git.repo", Params: &resource.Params{Params: map[string]string{"url": "secret"}}, WebhookToken: "tok1", Logs: "err"},
			{ID: 2, Canonical: "cron.timer", Params: &resource.Params{Params: map[string]string{"key": "val"}}, WebhookToken: "tok2", Logs: "log"},
		},
		ResourceTypes: []restype.ResourceType{
			{ID: 1, Name: "git", Source: "pikoci://git", Params: []string{"url"}},
		},
		SecretTypes: []sectype.SecretType{
			{ID: 1, Name: "vault", Source: "pikoci://vault", Params: []string{"path"}, Config: map[string]string{"token": "s3cret"}},
		},
		NotificationTypes: []notiftype.NotificationType{
			{ID: 1, Name: "slack", Source: "pikoci://slack", Params: []string{"channel"}},
		},
		Notifications: []notification.Notification{
			{ID: 1, Type: "slack", Name: "deploy", Canonical: "slack.deploy", On: []string{"success"}, Jobs: []string{"test"}},
		},
	}, nil)

	pp, err := s.S.GetPublicPipeline(ctx, "main", "my-pipeline")
	require.NoError(t, err)

	// Raw config should be removed
	assert.Nil(t, pp.Raw)

	// All resources should be sanitized
	for _, r := range pp.Resources {
		assert.Nil(t, r.Params)
		assert.Empty(t, r.WebhookToken)
		assert.Empty(t, r.Logs)
	}

	// Resource types should only have ID, Name, Source
	assert.Len(t, pp.ResourceTypes, 1)
	assert.Equal(t, "git", pp.ResourceTypes[0].Name)
	assert.Equal(t, "pikoci://git", pp.ResourceTypes[0].Source)
	assert.Nil(t, pp.ResourceTypes[0].Params)

	// Secret types should only have ID, Name, Source
	assert.Len(t, pp.SecretTypes, 1)
	assert.Equal(t, "vault", pp.SecretTypes[0].Name)
	assert.Nil(t, pp.SecretTypes[0].Params)
	assert.Nil(t, pp.SecretTypes[0].Config)

	// Notification types should only have ID, Name, Source
	assert.Len(t, pp.NotificationTypes, 1)
	assert.Equal(t, "slack", pp.NotificationTypes[0].Name)
	assert.Nil(t, pp.NotificationTypes[0].Params)

	// Notifications should keep non-sensitive fields
	assert.Len(t, pp.Notifications, 1)
	assert.Equal(t, "slack.deploy", pp.Notifications[0].Canonical)
	assert.Equal(t, []string{"success"}, pp.Notifications[0].On)
	assert.Equal(t, []string{"test"}, pp.Notifications[0].Jobs)
}

func TestGetPublicPipelineResource_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, err := s.S.GetPublicPipelineResource(ctx, "main", "my-pipeline", "git.repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found or not public")
}

func TestGetPublicPipelineJob_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, err := s.S.GetPublicPipelineJob(ctx, "main", "my-pipeline", "my-job")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found or not public")
}

func TestListPublicJobBuilds_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, _, err := s.S.ListPublicJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found or not public")
}

func TestListPublicResourceVersions_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, _, err := s.S.ListPublicResourceVersions(ctx, "main", "my-pipeline", "git.repo", nil, nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found or not public")
}

func TestPausePipeline_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.PausePipeline(ctx, "INVALID", "my-pipeline")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")

	err = s.S.PausePipeline(ctx, "main", "INVALID")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")
}

func TestUnpausePipeline_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.UnpausePipeline(ctx, "INVALID", "my-pipeline")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")

	err = s.S.UnpausePipeline(ctx, "main", "INVALID")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")
}

func TestCreatePipeline_InvalidTeamCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreatePipeline(ctx, "INVALID", "my-pipeline", []byte(`job "x" {}`), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")
}

func TestCreatePipeline_InvalidPipelineName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// "!!!" canonicalizes to empty string which is not a valid slug
	_, err := s.S.CreatePipeline(ctx, "main", "!!!", []byte(`job "x" {}`), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")
}

func TestCreatePipelineImage_DOT(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	// No builds for the job
	s.Builds.EXPECT().Filter(ctx, "main", "pikoci", "test", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.CreatePipelineImage(ctx, "main", hclConfig, nil, "dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestCreatePipelineImage_InvalidTeamCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreatePipelineImage(ctx, "INVALID", []byte(`job "x" {}`), nil, "dot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")
}

func TestCreatePipelineImage_InvalidFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	_, err := s.S.CreatePipelineImage(ctx, "main", hclConfig, nil, "bmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid image format")
}

func TestCreatePipelineImage_BadConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreatePipelineImage(ctx, "main", []byte(`not valid HCL!!!`), nil, "dot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read Pipeline")
}

func TestCreatePipelineImage_FormatFromExtension(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	// No builds for the job
	s.Builds.EXPECT().Filter(ctx, "main", "pikoci", "test", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.CreatePipelineImage(ctx, "main", hclConfig, nil, "image.dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestGetPublicPipelineImage_DOT(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron"},
		},
	}

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.GetPublicPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestGetPublicPipelineImage_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(nil, assert.AnError)

	_, err := s.S.GetPublicPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline not found or not public")
}

func TestGetPublicPipelineImage_InvalidFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
	}

	s.Pipelines.EXPECT().FindPublic(ctx, "main", "my-pipeline").Return(pp, nil)

	_, err := s.S.GetPublicPipelineImage(ctx, "main", "my-pipeline", "bmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid image format")
}

func TestGetPipelineImage_InvalidTeamCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetPipelineImage(ctx, "INVALID", "my-pipeline", "dot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")
}

func TestGetPipelineImage_InvalidPipelineCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetPipelineImage(ctx, "main", "INVALID", "dot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")
}

func TestGetPipelineImage_InvalidFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "bmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid image format")
}

func TestGetPipelineImage_DOT(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestGetPipelineImage_FormatFromExtension(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs:      []job.Job{},
		Resources: []resource.Resource{},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "image.dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestGetPipelineImage_EmptyFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs:      []job.Job{},
		Resources: []resource.Resource{},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestGetPipelineImage_WithBuildStatuses(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				Name: "succeeded-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
			{
				Name: "failed-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
			{
				Name:   "paused-job",
				Paused: true,
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
			{
				Name: "running-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
					{Type: job.StepTypePut, Put: &job.PutStep{Type: "git", Name: "repo"}},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron"},
			{ID: 2, Canonical: "git.repo", Name: "repo", Type: "git"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	// succeeded-job has a terminal build
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "succeeded-job", gomock.Any(), gomock.Any(), gomock.Any()).Return([]*build.Build{
		{ID: 1, BuildNumber: "1", Status: build.Succeeded},
	}, nil)
	// failed-job has a failed build
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "failed-job", gomock.Any(), gomock.Any(), gomock.Any()).Return([]*build.Build{
		{ID: 2, BuildNumber: "1", Status: build.Failed},
	}, nil)
	// paused-job has no builds
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "paused-job", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	// running-job has a running build and a pending build
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "running-job", gomock.Any(), gomock.Any(), gomock.Any()).Return([]*build.Build{
		{ID: 3, BuildNumber: "1", Status: build.Started},
	}, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)
	result := string(img)
	assert.Contains(t, result, "graph")
	assert.Contains(t, result, "succeeded-job")
	assert.Contains(t, result, "failed-job")
	assert.Contains(t, result, "paused-job")
	assert.Contains(t, result, "running-job")
}

func TestGetPipelineImage_WithPassedConstraints(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				Name: "upstream",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
			{
				Name: "downstream",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer", Passed: []string{"upstream"}}},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "upstream", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "downstream", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)
	result := string(img)
	assert.Contains(t, result, "upstream")
	assert.Contains(t, result, "downstream")
}

func TestGetPipelineImage_ResourceWithError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", Logs: "check error: something failed"},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestGetPipelineImage_PinnedResource(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pinnedID := uint32(42)
	pp := &pipeline.Pipeline{
		ID:        1,
		Name:      "my-pipeline",
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{
				Name: "my-job",
				Plan: []job.PlanStep{
					{Type: job.StepTypeGet, Get: &job.GetStep{Type: "cron", Name: "timer"}},
				},
			},
		},
		Resources: []resource.Resource{
			{ID: 1, Canonical: "cron.timer", Name: "timer", Type: "cron", PinnedVersionID: &pinnedID},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	img, err := s.S.GetPipelineImage(ctx, "main", "my-pipeline", "dot")
	require.NoError(t, err)
	assert.Contains(t, string(img), "graph")
}

func TestCreatePipeline_WithRunnerType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
runner_type "docker" {
  run {
    path = "docker"
    args = ["run"]
  }
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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
	s.Jobs.EXPECT().Create(ctx, "main", "runner-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "runner-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Runners.EXPECT().Create(ctx, "main", "runner-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "runner-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "runner-pipeline", Canonical: "runner-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "runner-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_WithNotificationTypeAndNotification(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
notification_type "slack" {
  params = ["channel"]
  notify "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo notify"]
  }
}

notification "slack" "deploy-notif" {
  on = ["success", "failure"]
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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
	s.Jobs.EXPECT().Create(ctx, "main", "notif-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "notif-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.NotificationTypes.EXPECT().Create(ctx, "main", "notif-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Notifications.EXPECT().Create(ctx, "main", "notif-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "notif-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "notif-pipeline", Canonical: "notif-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "notif-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_InvalidCheckInterval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "timer" {
  check_interval = "invalid-interval"
}

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

	// Pipeline and Job are created before the resource validation fails
	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "interval-pipeline", gomock.Any()).Return(uint32(1), nil)

	_, err := s.S.CreatePipeline(ctx, "main", "interval-pipeline", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid check_interval")
}

func TestCreatePipeline_NumberVariable(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
variable "count" {
  type    = number
  default = 5
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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
	s.Jobs.EXPECT().Create(ctx, "main", "numvar-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "numvar-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "numvar-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "numvar-pipeline", Canonical: "numvar-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "numvar-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_BoolVariable(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
variable "debug" {
  type    = bool
  default = false
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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
	s.Jobs.EXPECT().Create(ctx, "main", "boolvar-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "boolvar-pipeline", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "boolvar-pipeline").Return(&pipeline.Pipeline{ID: 1, Name: "boolvar-pipeline", Canonical: "boolvar-pipeline"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "boolvar-pipeline", hclConfig, nil)
	require.NoError(t, err)
}

func TestCreatePipeline_NumberVariableOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
variable "count" {
  type    = number
  default = 5
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	vars := map[string]interface{}{"count": float64(10)}

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "numvar-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "numvar-override", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "numvar-override").Return(&pipeline.Pipeline{ID: 1, Name: "numvar-override", Canonical: "numvar-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "numvar-override", hclConfig, vars)
	require.NoError(t, err)
}

func TestCreatePipeline_BoolVariableOverride(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
variable "debug" {
  type    = bool
  default = false
}

resource "cron" "timer" {
  check_interval = "@every 1h"
}

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

	vars := map[string]interface{}{"debug": true}

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Jobs.EXPECT().Create(ctx, "main", "boolvar-override", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "boolvar-override", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "boolvar-override").Return(&pipeline.Pipeline{ID: 1, Name: "boolvar-override", Canonical: "boolvar-override"}, nil)

	_, err := s.S.CreatePipeline(ctx, "main", "boolvar-override", hclConfig, vars)
	require.NoError(t, err)
}
