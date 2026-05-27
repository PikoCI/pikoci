package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/utils"
)

func TestPipeline_ResourceType(t *testing.T) {
	pp := &pipeline.Pipeline{
		ResourceTypes: []restype.ResourceType{
			{Name: "custom"},
		},
	}

	t.Run("finds existing resource type", func(t *testing.T) {
		rt, ok := pp.ResourceType("custom")
		assert.True(t, ok)
		assert.Equal(t, "custom", rt.Name)
	})

	t.Run("returns built-in cron type", func(t *testing.T) {
		rt, ok := pp.ResourceType("cron")
		assert.True(t, ok)
		assert.Equal(t, "cron", rt.Name)
		assert.Equal(t, "exec", rt.Check.Runner)
	})

	t.Run("returns built-in git type", func(t *testing.T) {
		rt, ok := pp.ResourceType("git")
		assert.True(t, ok)
		assert.Equal(t, "git", rt.Name)
		assert.Equal(t, "exec", rt.Check.Runner)
		assert.Equal(t, "exec", rt.Pull.Runner)
		assert.Contains(t, rt.Params, "url")
		assert.Contains(t, rt.Params, "token")
	})

	t.Run("inline overrides built-in", func(t *testing.T) {
		pp2 := &pipeline.Pipeline{
			ResourceTypes: []restype.ResourceType{
				{Name: "git", Params: []string{"url"}},
			},
		}
		rt, ok := pp2.ResourceType("git")
		assert.True(t, ok)
		assert.Equal(t, []string{"url"}, rt.Params)
	})

	t.Run("returns false for unknown type", func(t *testing.T) {
		_, ok := pp.ResourceType("unknown")
		assert.False(t, ok)
	})
}

func TestPipeline_Resource(t *testing.T) {
	pp := &pipeline.Pipeline{
		Resources: []resource.Resource{
			{Canonical: "git.my-repo", Name: "my-repo"},
			{Canonical: "cron.timer", Name: "timer"},
		},
	}

	t.Run("finds existing resource", func(t *testing.T) {
		r, ok := pp.Resource("git.my-repo")
		assert.True(t, ok)
		assert.Equal(t, "my-repo", r.Name)
	})

	t.Run("returns false for unknown resource", func(t *testing.T) {
		_, ok := pp.Resource("nonexistent")
		assert.False(t, ok)
	})
}

func TestPipeline_Runner(t *testing.T) {
	pp := &pipeline.Pipeline{
		Runners: []runner.Runner{
			{Name: "custom"},
		},
	}

	t.Run("finds existing runner", func(t *testing.T) {
		r, ok := pp.Runner("custom")
		assert.True(t, ok)
		assert.Equal(t, "custom", r.Name)
	})

	t.Run("returns built-in exec runner", func(t *testing.T) {
		r, ok := pp.Runner("exec")
		assert.True(t, ok)
		assert.Equal(t, "exec", r.Name)
		assert.Equal(t, "$path", r.Run.Path)
		assert.Equal(t, []string{"$args"}, r.Run.Args)
	})

	t.Run("returns built-in docker runner", func(t *testing.T) {
		r, ok := pp.Runner("docker")
		assert.True(t, ok)
		assert.Equal(t, "docker", r.Name)
		assert.Equal(t, "docker", r.Run.Path)
		assert.Contains(t, r.Run.Args, "run")
	})

	t.Run("inline overrides built-in", func(t *testing.T) {
		pp2 := &pipeline.Pipeline{
			Runners: []runner.Runner{
				{Name: "docker", Run: utils.RunCommand{Path: "/custom/docker"}},
			},
		}
		r, ok := pp2.Runner("docker")
		assert.True(t, ok)
		assert.Equal(t, "/custom/docker", r.Run.Path)
	})

	t.Run("returns false for unknown runner", func(t *testing.T) {
		_, ok := pp.Runner("unknown")
		assert.False(t, ok)
	})
}

func TestParseServicesFromRaw_WithVarReferences(t *testing.T) {
	raw := []byte(`
variable "timeout" {
  type    = string
  default = "5m"
}

service_type "mydb" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "docker run -d mydb"]
  }
  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "echo ready"]
    interval = "2s"
    timeout  = var.timeout
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "docker rm -f mydb"]
  }
}
`)

	svcs, err := pipeline.ParseServicesFromRaw(context.Background(), raw)
	require.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Equal(t, "mydb", svcs[0].Name)
	require.NotNil(t, svcs[0].ReadyCheck)
	assert.Equal(t, "5m", svcs[0].ReadyCheck.Timeout)
}

func TestParseServicesFromRaw_NoVariables(t *testing.T) {
	raw := []byte(`
service_type "simple" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo start"]
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo stop"]
  }
}
`)

	svcs, err := pipeline.ParseServicesFromRaw(context.Background(), raw)
	require.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Equal(t, "simple", svcs[0].Name)
}

func TestParseServicesFromRaw_SecretVariable(t *testing.T) {
	raw := []byte(`
secret_type "env" {
  source = "pikoci://file"
  path   = "/etc/test.env"
}

variable "db_pass" {
  type = string
  secret "env" {
    key = "DB_PASSWORD"
  }
}

service_type "db" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "docker run -e PASS=${var.db_pass} mydb"]
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "docker rm -f mydb"]
  }
}
`)

	svcs, err := pipeline.ParseServicesFromRaw(context.Background(), raw)
	require.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Equal(t, "db", svcs[0].Name)
}

func TestParseServicesFromRaw_EmptyRaw(t *testing.T) {
	svcs, err := pipeline.ParseServicesFromRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, svcs)
}
