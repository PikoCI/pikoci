package pikoci_test

import (
	"context"
	"testing"

	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCreatePipeline_ForEachSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = toset(["a", "b", "c"])
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["testing ${each.value}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	// Expect 3 job creates: test--a, test--b, test--c
	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(len(createdJobs)), nil
		}).Times(3)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 3)

	// Jobs should be sorted by key
	assert.Equal(t, "test--a", createdJobs[0].Name)
	assert.Equal(t, "test--b", createdJobs[1].Name)
	assert.Equal(t, "test--c", createdJobs[2].Name)

	// Verify ForEachGroup and ForEachKey
	for _, j := range createdJobs {
		assert.Equal(t, "test", j.ForEachGroup)
	}
	assert.Equal(t, "a", createdJobs[0].ForEachKey)
	assert.Equal(t, "b", createdJobs[1].ForEachKey)
	assert.Equal(t, "c", createdJobs[2].ForEachKey)

	// Verify each.value interpolation in task args
	assert.Contains(t, createdJobs[0].Plan[1].Task.Run.Args[0], "testing a")
	assert.Contains(t, createdJobs[1].Plan[1].Task.Run.Args[0], "testing b")
	assert.Contains(t, createdJobs[2].Plan[1].Task.Run.Args[0], "testing c")
}

func TestCreatePipeline_ForEachSetWithDots(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = toset(["1.21", "1.22"])
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["go ${each.value}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(len(createdJobs)), nil
		}).Times(2)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 2)
	// Dots should be slugified to hyphens
	assert.Equal(t, "test--1-21", createdJobs[0].Name)
	assert.Equal(t, "test--1-22", createdJobs[1].Name)

	// Keys should be original (not slugified)
	assert.Equal(t, "1.21", createdJobs[0].ForEachKey)
	assert.Equal(t, "1.22", createdJobs[1].ForEachKey)
}

func TestCreatePipeline_ForEachMap(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = {
    "old" = "1.21"
    "new" = "1.22"
  }
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["${each.key}: ${each.value}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(len(createdJobs)), nil
		}).Times(2)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 2)
	assert.Equal(t, "test--new", createdJobs[0].Name)
	assert.Equal(t, "test--old", createdJobs[1].Name)

	assert.Equal(t, "test", createdJobs[0].ForEachGroup)
	assert.Equal(t, "new", createdJobs[0].ForEachKey)
	assert.Equal(t, "old", createdJobs[1].ForEachKey)

	// Verify each.key and each.value interpolation
	assert.Contains(t, createdJobs[0].Plan[1].Task.Run.Args[0], "new: 1.22")
	assert.Contains(t, createdJobs[1].Plan[1].Task.Run.Args[0], "old: 1.21")
}

func TestCreatePipeline_Matrix(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  matrix {
    go = ["1.21", "1.22"]
    os = ["linux", "darwin"]
  }
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["go ${each.value.go} on ${each.value.os}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(len(createdJobs)), nil
		}).Times(4) // 2 go x 2 os = 4

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 4)

	// Verify all 4 combinations exist
	names := make(map[string]bool)
	for _, j := range createdJobs {
		names[j.Name] = true
		assert.Equal(t, "test", j.ForEachGroup)
	}
	assert.True(t, names["test--1-21-darwin"])
	assert.True(t, names["test--1-21-linux"])
	assert.True(t, names["test--1-22-darwin"])
	assert.True(t, names["test--1-22-linux"])
}

func TestCreatePipeline_ForEachWithRegularJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "build" {
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["building"]
    }
  }
}

job "test" {
  for_each = toset(["unit", "integration"])
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["${each.value}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(len(createdJobs)), nil
		}).Times(3) // 1 regular + 2 for_each

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 3)
	assert.Equal(t, "build", createdJobs[0].Name)
	assert.Equal(t, "", createdJobs[0].ForEachGroup) // regular job has no group
	assert.Equal(t, "test--integration", createdJobs[1].Name)
	assert.Equal(t, "test--unit", createdJobs[2].Name)
}

func TestCreatePipeline_ForEachMutuallyExclusive(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = toset(["a"])
  matrix {
    x = ["1"]
  }
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestCreatePipeline_ForEachNameCollision(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "build--linux" {
  get "cron" "tick" {
    trigger = true
  }
}

job "build" {
  for_each = toset(["linux", "mac"])
  get "cron" "tick" {
    trigger = true
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already used by another job")
}

func TestCreatePipeline_ForEachEmptySet(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = toset([])
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no instances")
}

func TestCreatePipeline_ForEachNoForEach(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Regular pipeline without for_each should work as before
	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "build" {
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["hello"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(1), nil
		}).Times(1)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 1)
	assert.Equal(t, "build", createdJobs[0].Name)
	assert.Equal(t, "", createdJobs[0].ForEachGroup)
	assert.Equal(t, "", createdJobs[0].ForEachKey)
}

func TestCreatePipeline_ForEachSingleElement(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = toset(["only"])
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["${each.value}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(1), nil
		}).Times(1)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 1)
	assert.Equal(t, "test--only", createdJobs[0].Name)
	assert.Equal(t, "test", createdJobs[0].ForEachGroup)
	assert.Equal(t, "only", createdJobs[0].ForEachKey)
}

func TestCreatePipeline_MatrixSingleAxis(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  matrix {
    go = ["1.21", "1.22"]
  }
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["go ${each.value.go}"]
    }
  }
}
`)

	s.Pipelines.EXPECT().Create(ctx, "main", gomock.Any()).Return(uint32(1), nil)
	s.Resources.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).Return(uint32(1), nil)
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipe").Return(&pipeline.Pipeline{ID: 1}, nil)

	var createdJobs []job.Job
	s.Jobs.EXPECT().Create(ctx, "main", "my-pipe", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pn string, j job.Job) (uint32, error) {
			createdJobs = append(createdJobs, j)
			return uint32(len(createdJobs)), nil
		}).Times(2)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.NoError(t, err)

	require.Len(t, createdJobs, 2)
	assert.Equal(t, "test--1-21", createdJobs[0].Name)
	assert.Equal(t, "test--1-22", createdJobs[1].Name)
}

func TestCreatePipelineImage_ForEachPassedFanIn(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

resource "artifact" "output" {
  params {
    dir = "output"
  }
}

job "gen" {
  get "cron" "tick" {
    trigger = true
  }
  task "create" {
    run "exec" {
      path = "/bin/echo"
      args = ["gen"]
    }
  }
  put "artifact" "output" {
    dir = "out"
  }
}

job "validate" {
  for_each = toset(["lint", "vet"])

  get "artifact" "output" {
    trigger = true
    passed  = ["gen"]
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["${each.value}"]
    }
  }
}

job "deploy" {
  get "artifact" "output" {
    trigger = true
    passed  = ["validate"]
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
      args = ["deploy"]
    }
  }
}
`)

	// Expect FilterByPipeline for all jobs in a single query
	s.Builds.EXPECT().FilterByPipeline(ctx, "main", "pikoci", ([]build.Status)(nil)).
		Return(nil, nil)
	s.Resources.EXPECT().LatestVersionByResources(ctx, "main", "pikoci").
		Return(map[string]*resource.Version{}, nil)

	img, err := s.S.CreatePipelineImage(ctx, "main", hclConfig, nil, "dot")
	require.NoError(t, err)

	dot := string(img)
	// All expanded job names should appear as nodes
	assert.Contains(t, dot, `"validate--lint"`)
	assert.Contains(t, dot, `"validate--vet"`)
	assert.Contains(t, dot, `"deploy"`)
	assert.Contains(t, dot, `"gen"`)
	// The group name "validate" should NOT appear as a node
	// (only expanded instances should)
}

func TestCreatePipeline_ForEachInvalidType(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// A raw list (not toset()) should be rejected
	hclConfig := []byte(`
resource "cron" "tick" {
  check_interval = "@every 1m"
}

job "test" {
  for_each = ["a", "b"]
  get "cron" "tick" {
    trigger = true
  }
  task "run" {
    run "exec" {
      path = "/bin/echo"
    }
  }
}
`)

	_, err := s.S.CreatePipeline(ctx, "main", "my-pipe", hclConfig, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "for_each must be a set")
}
