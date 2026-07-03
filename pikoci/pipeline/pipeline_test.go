package pipeline_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/service"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/zclconf/go-cty/cty"
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

func TestParseServicesFromRaw_CustomServiceType(t *testing.T) {
	raw := []byte(`
service_type "mydb" {
  start "exec" {
    path = "/bin/sh"
    args = ["-c", "echo start"]
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-c", "echo stop"]
  }
  ready_check "exec" {
    path = "/bin/sh"
    args = ["-c", "echo ready"]
    interval = "1s"
    timeout  = "30s"
  }
  params = ["version", "port"]
}
`)
	svcs, err := pipeline.ParseServicesFromRaw(context.Background(), raw)
	require.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Equal(t, "mydb", svcs[0].Name)
	assert.Equal(t, "exec", svcs[0].Start.Runner)
	assert.Equal(t, "exec", svcs[0].Stop.Runner)
	require.NotNil(t, svcs[0].ReadyCheck)
	assert.Equal(t, "exec", svcs[0].ReadyCheck.Runner)
	assert.Equal(t, "1s", svcs[0].ReadyCheck.Interval)
	assert.Equal(t, "30s", svcs[0].ReadyCheck.Timeout)
}

func TestParseServicesFromRaw_WithNumberAndBoolVars(t *testing.T) {
	raw := []byte(`
variable "port" {
  type    = number
  default = 5432
}

variable "debug" {
  type    = bool
  default = true
}

variable "no_default_num" {
  type = number
}

variable "no_default_bool" {
  type = bool
}

service_type "db" {
  start "exec" {
    path = "/bin/sh"
    args = ["-c", "echo start"]
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-c", "echo stop"]
  }
}
`)
	svcs, err := pipeline.ParseServicesFromRaw(context.Background(), raw)
	require.NoError(t, err)
	require.Len(t, svcs, 1)
	assert.Equal(t, "db", svcs[0].Name)
}

func TestPipeline_SecretType(t *testing.T) {
	pp := &pipeline.Pipeline{
		SecretTypes: []sectype.SecretType{
			{Name: "vault", Params: []string{"addr", "token"}},
			{Name: "file", Params: []string{"path"}},
		},
	}

	t.Run("finds existing secret type", func(t *testing.T) {
		st, ok := pp.SecretType("vault")
		assert.True(t, ok)
		assert.Equal(t, "vault", st.Name)
		assert.Equal(t, []string{"addr", "token"}, st.Params)
	})

	t.Run("finds second secret type", func(t *testing.T) {
		st, ok := pp.SecretType("file")
		assert.True(t, ok)
		assert.Equal(t, "file", st.Name)
	})

	t.Run("returns false for unknown secret type", func(t *testing.T) {
		_, ok := pp.SecretType("unknown")
		assert.False(t, ok)
	})

	t.Run("returns false on empty pipeline", func(t *testing.T) {
		empty := &pipeline.Pipeline{}
		_, ok := empty.SecretType("vault")
		assert.False(t, ok)
	})
}

func TestPipeline_NotificationType(t *testing.T) {
	pp := &pipeline.Pipeline{
		NotificationTypes: []notiftype.NotificationType{
			{Name: "slack"},
			{Name: "email"},
		},
	}

	t.Run("finds existing notification type", func(t *testing.T) {
		nt, ok := pp.NotificationType("slack")
		assert.True(t, ok)
		assert.Equal(t, "slack", nt.Name)
	})

	t.Run("finds second notification type", func(t *testing.T) {
		nt, ok := pp.NotificationType("email")
		assert.True(t, ok)
		assert.Equal(t, "email", nt.Name)
	})

	t.Run("returns false for unknown notification type", func(t *testing.T) {
		_, ok := pp.NotificationType("unknown")
		assert.False(t, ok)
	})

	t.Run("returns false on empty pipeline (builtin returns nil)", func(t *testing.T) {
		empty := &pipeline.Pipeline{}
		_, ok := empty.NotificationType("slack")
		assert.False(t, ok)
	})

	t.Run("inline overrides built-in", func(t *testing.T) {
		pp2 := &pipeline.Pipeline{
			NotificationTypes: []notiftype.NotificationType{
				{Name: "mynotif", Params: []string{"url"}},
			},
		}
		nt, ok := pp2.NotificationType("mynotif")
		assert.True(t, ok)
		assert.Equal(t, []string{"url"}, nt.Params)
	})
}

func TestPipeline_Notification(t *testing.T) {
	pp := &pipeline.Pipeline{
		Notifications: []notification.Notification{
			{Canonical: "slack.deploy-alerts", Name: "deploy-alerts", Type: "slack"},
			{Canonical: "email.ops", Name: "ops", Type: "email"},
		},
	}

	t.Run("finds existing notification", func(t *testing.T) {
		n, ok := pp.Notification("slack.deploy-alerts")
		assert.True(t, ok)
		assert.Equal(t, "deploy-alerts", n.Name)
		assert.Equal(t, "slack", n.Type)
	})

	t.Run("finds second notification", func(t *testing.T) {
		n, ok := pp.Notification("email.ops")
		assert.True(t, ok)
		assert.Equal(t, "ops", n.Name)
	})

	t.Run("returns false for unknown notification", func(t *testing.T) {
		_, ok := pp.Notification("unknown.notif")
		assert.False(t, ok)
	})

	t.Run("returns false on empty pipeline", func(t *testing.T) {
		empty := &pipeline.Pipeline{}
		_, ok := empty.Notification("slack.deploy-alerts")
		assert.False(t, ok)
	})
}

func TestPipeline_Service(t *testing.T) {
	pp := &pipeline.Pipeline{
		Services: []service.Service{
			{Name: "postgres", Params: []string{"version", "port"}},
			{Name: "redis"},
		},
	}

	t.Run("finds existing service", func(t *testing.T) {
		s, ok := pp.Service("postgres")
		assert.True(t, ok)
		assert.Equal(t, "postgres", s.Name)
		assert.Equal(t, []string{"version", "port"}, s.Params)
	})

	t.Run("finds second service", func(t *testing.T) {
		s, ok := pp.Service("redis")
		assert.True(t, ok)
		assert.Equal(t, "redis", s.Name)
	})

	t.Run("returns false for unknown service", func(t *testing.T) {
		_, ok := pp.Service("unknown")
		assert.False(t, ok)
	})

	t.Run("returns false on empty pipeline", func(t *testing.T) {
		empty := &pipeline.Pipeline{}
		_, ok := empty.Service("postgres")
		assert.False(t, ok)
	})
}

func TestParseSecretVarsFromRaw(t *testing.T) {
	t.Run("parses secret variables", func(t *testing.T) {
		raw := []byte(`
variable "db_pass" {
  type = string
  secret "vault" {
    path = "secret/data/db"
    key  = "password"
  }
}

variable "api_key" {
  type = string
  secret "file" {
    key = "API_KEY"
  }
}

variable "normal" {
  type    = string
  default = "hello"
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		require.Len(t, secretVars, 2)

		dbPass, ok := secretVars["db_pass"]
		require.True(t, ok)
		assert.Equal(t, "vault", dbPass.Type)
		assert.Equal(t, "secret/data/db", dbPass.Path)
		assert.Equal(t, "password", dbPass.Key)

		apiKey, ok := secretVars["api_key"]
		require.True(t, ok)
		assert.Equal(t, "file", apiKey.Type)
		assert.Equal(t, "API_KEY", apiKey.Key)
	})

	t.Run("returns nil for empty raw", func(t *testing.T) {
		secretVars, err := pipeline.ParseSecretVarsFromRaw(nil, nil)
		require.NoError(t, err)
		assert.Nil(t, secretVars)
	})

	t.Run("returns nil when no secret variables", func(t *testing.T) {
		raw := []byte(`
variable "normal" {
  type    = string
  default = "hello"
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		assert.Nil(t, secretVars)
	})

	t.Run("excludes overridden variables", func(t *testing.T) {
		raw := []byte(`
variable "db_pass" {
  type = string
  secret "vault" {
    path = "secret/data/db"
    key  = "password"
  }
}

variable "api_key" {
  type = string
  secret "file" {
    key = "API_KEY"
  }
}
`)
		vars := map[string]interface{}{
			"db_pass": "override-value",
		}
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, vars)
		require.NoError(t, err)
		require.Len(t, secretVars, 1)

		_, ok := secretVars["db_pass"]
		assert.False(t, ok)

		apiKey, ok := secretVars["api_key"]
		require.True(t, ok)
		assert.Equal(t, "file", apiKey.Type)
	})

	t.Run("returns nil when all secrets overridden", func(t *testing.T) {
		raw := []byte(`
variable "db_pass" {
  type = string
  secret "vault" {
    path = "secret/data/db"
    key  = "password"
  }
}
`)
		vars := map[string]interface{}{
			"db_pass": "override-value",
		}
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, vars)
		require.NoError(t, err)
		assert.Nil(t, secretVars)
	})
}

func TestParseSecretVarsFromRaw_SecretChaining(t *testing.T) {
	t.Run("path references variable with default", func(t *testing.T) {
		raw := []byte(`
variable "base_path" {
  type    = string
  default = "/etc/secrets"
}

variable "key_content" {
  type = string
  secret "file" {
    path = var.base_path
    key  = "content"
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		require.Len(t, secretVars, 1)

		kc, ok := secretVars["key_content"]
		require.True(t, ok)
		assert.Equal(t, "file", kc.Type)
		assert.Equal(t, "/etc/secrets", kc.Path)
		assert.Equal(t, "content", kc.Key)
	})

	t.Run("path references secret-backed variable contains placeholder", func(t *testing.T) {
		raw := []byte(`
variable "key_path" {
  type = string
  secret "env" {
    key = "KEY_FILE"
  }
}

variable "key_content" {
  type = string
  secret "file" {
    path = var.key_path
    key  = "content"
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		require.Len(t, secretVars, 2)

		kc, ok := secretVars["key_content"]
		require.True(t, ok)
		assert.Equal(t, "file", kc.Type)
		assert.Contains(t, kc.Path, "__pikoci_secret:env::KEY_FILE__")
		assert.Equal(t, "content", kc.Key)
	})

	t.Run("path with string interpolation", func(t *testing.T) {
		raw := []byte(`
variable "key_path" {
  type = string
  secret "env" {
    key = "KEY_DIR"
  }
}

variable "key_content" {
  type = string
  secret "file" {
    path = "${var.key_path}/subdir"
    key  = "content"
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)

		kc, ok := secretVars["key_content"]
		require.True(t, ok)
		assert.Equal(t, "__pikoci_secret:env::KEY_DIR__/subdir", kc.Path)
	})

	t.Run("key references secret-backed variable", func(t *testing.T) {
		raw := []byte(`
variable "key_name" {
  type = string
  secret "env" {
    key = "SECRET_KEY_NAME"
  }
}

variable "value" {
  type = string
  secret "vault" {
    path = "secret/data/app"
    key  = var.key_name
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)

		v, ok := secretVars["value"]
		require.True(t, ok)
		assert.Equal(t, "vault", v.Type)
		assert.Equal(t, "secret/data/app", v.Path)
		assert.Contains(t, v.Key, "__pikoci_secret:env::SECRET_KEY_NAME__")
	})

	t.Run("no path just key works as before", func(t *testing.T) {
		raw := []byte(`
variable "api_key" {
  type = string
  secret "env" {
    key = "API_KEY"
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		require.Len(t, secretVars, 1)

		ak, ok := secretVars["api_key"]
		require.True(t, ok)
		assert.Equal(t, "env", ak.Type)
		assert.Equal(t, "", ak.Path)
		assert.Equal(t, "API_KEY", ak.Key)
	})

	t.Run("dependent declared before dependency still works", func(t *testing.T) {
		raw := []byte(`
variable "key_content" {
  type = string
  secret "file" {
    path = var.key_path
    key  = "content"
  }
}

variable "key_path" {
  type = string
  secret "env" {
    key = "KEY_FILE"
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		require.Len(t, secretVars, 2)

		kc, ok := secretVars["key_content"]
		require.True(t, ok)
		assert.Contains(t, kc.Path, "__pikoci_secret:env::KEY_FILE__")
	})

	t.Run("path references undefined variable returns error", func(t *testing.T) {
		raw := []byte(`
variable "key_content" {
  type = string
  secret "file" {
    path = var.nonexistent
    key  = "content"
  }
}
`)
		_, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to evaluate secret path")
	})

	t.Run("path references number variable returns type error", func(t *testing.T) {
		raw := []byte(`
variable "some_number" {
  type    = number
  default = 42
}

variable "key_content" {
  type = string
  secret "file" {
    path = var.some_number
    key  = "content"
  }
}
`)
		_, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a string")
	})

	t.Run("override in vars breaks chain", func(t *testing.T) {
		raw := []byte(`
variable "key_path" {
  type = string
  secret "env" {
    key = "KEY_FILE"
  }
}

variable "key_content" {
  type = string
  secret "file" {
    path = var.key_path
    key  = "content"
  }
}
`)
		// Override key_path — it's no longer secret-backed, so key_content's
		// path resolves to the override value directly (no placeholder).
		vars := map[string]interface{}{
			"key_path": "/override/path",
		}
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, vars)
		require.NoError(t, err)
		require.Len(t, secretVars, 1)

		// key_path is excluded (overridden)
		_, ok := secretVars["key_path"]
		assert.False(t, ok)

		// key_content's path resolves to the override value
		kc, ok := secretVars["key_content"]
		require.True(t, ok)
		assert.Equal(t, "/override/path", kc.Path)
	})

	t.Run("literal path still works unchanged", func(t *testing.T) {
		raw := []byte(`
variable "db_pass" {
  type = string
  secret "vault" {
    path = "secret/data/db"
    key  = "password"
  }
}
`)
		secretVars, err := pipeline.ParseSecretVarsFromRaw(raw, nil)
		require.NoError(t, err)
		require.Len(t, secretVars, 1)

		dp, ok := secretVars["db_pass"]
		require.True(t, ok)
		assert.Equal(t, "secret/data/db", dp.Path)
		assert.Equal(t, "password", dp.Key)
	})
}

func TestTypeEvalContext(t *testing.T) {
	ectx := pipeline.TypeEvalContext()
	require.NotNil(t, ectx)
	require.NotNil(t, ectx.Variables)
	assert.Equal(t, cty.StringVal("string"), ectx.Variables["string"])
	assert.Equal(t, cty.StringVal("number"), ectx.Variables["number"])
	assert.Equal(t, cty.StringVal("bool"), ectx.Variables["bool"])
}
