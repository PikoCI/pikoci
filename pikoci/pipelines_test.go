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
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
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
