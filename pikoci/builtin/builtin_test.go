package builtin_test

import (
	"testing"

	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/builtin"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/sectype"
)

func TestResourceTypes(t *testing.T) {
	rts := builtin.ResourceTypes()
	require.NotEmpty(t, rts)

	t.Run("cron", func(t *testing.T) {
		rt, ok := rts["cron"]
		require.True(t, ok)
		assert.Equal(t, "cron", rt.Name)
		assert.Equal(t, "exec", rt.Check.Runner)
		assert.False(t, rt.Cache)
	})

	t.Run("git", func(t *testing.T) {
		rt, ok := rts["git"]
		require.True(t, ok)
		assert.Equal(t, "git", rt.Name)
		assert.Equal(t, "exec", rt.Check.Runner)
		assert.Equal(t, "exec", rt.Pull.Runner)
		assert.Equal(t, "exec", rt.Push.Runner)
		assert.True(t, rt.Cache)
		assert.Contains(t, rt.Params, "url")
		assert.Contains(t, rt.Params, "name")
		assert.Contains(t, rt.Params, "token")
		assert.Contains(t, rt.Params, "branch")
		assert.Contains(t, rt.Params, "pr")
	})

	t.Run("artifact", func(t *testing.T) {
		rt, ok := rts["artifact"]
		require.True(t, ok)
		assert.Equal(t, "artifact", rt.Name)
		assert.True(t, rt.Cache)
		assert.Equal(t, "exec", rt.Check.Runner)
		assert.Equal(t, "exec", rt.Pull.Runner)
		assert.Equal(t, "exec", rt.Push.Runner)
		assert.Contains(t, rt.Params, "dir")
		assert.Contains(t, rt.Params, "base_dir")
	})

	t.Run("fs", func(t *testing.T) {
		rt, ok := rts["fs"]
		require.True(t, ok)
		assert.Equal(t, "fs", rt.Name)
		assert.Equal(t, "exec", rt.Check.Runner)
		assert.Equal(t, "exec", rt.Pull.Runner)
		assert.False(t, rt.Cache)
		assert.Contains(t, rt.Params, "path")
	})

	t.Run("trigger", func(t *testing.T) {
		rt, ok := rts["trigger"]
		require.True(t, ok)
		assert.Equal(t, "trigger", rt.Name)
		assert.Equal(t, "pikoci://trigger", rt.Source)
		assert.Nil(t, rt.Check)
		assert.Nil(t, rt.Pull)
		assert.Nil(t, rt.Push)
		assert.Empty(t, rt.Params)
	})
}

func TestRunners(t *testing.T) {
	rus := builtin.Runners()
	require.NotEmpty(t, rus)

	t.Run("exec", func(t *testing.T) {
		ru, ok := rus["exec"]
		require.True(t, ok)
		assert.Equal(t, "exec", ru.Name)
		assert.Equal(t, "$path", ru.Run.Path)
		assert.Equal(t, []string{"$args"}, ru.Run.Args)
	})

	t.Run("docker", func(t *testing.T) {
		ru, ok := rus["docker"]
		require.True(t, ok)
		assert.Equal(t, "docker", ru.Name)
		assert.Equal(t, "docker", ru.Run.Path)
		assert.Contains(t, ru.Run.Args, "run")
		assert.Contains(t, ru.Run.Args, "--rm")
	})

	t.Run("shell", func(t *testing.T) {
		ru, ok := rus["shell"]
		require.True(t, ok)
		assert.Equal(t, "shell", ru.Name)
		assert.Equal(t, "$shell", ru.Run.Path)
		assert.Equal(t, []string{"-ec", "$cmd"}, ru.Run.Args)
	})
}

func TestResourceTypeHCL(t *testing.T) {
	t.Run("existing", func(t *testing.T) {
		data, ok := builtin.ResourceTypeHCL("git")
		assert.True(t, ok)
		assert.NotEmpty(t, data)
	})

	t.Run("nonexistent", func(t *testing.T) {
		_, ok := builtin.ResourceTypeHCL("nonexistent")
		assert.False(t, ok)
	})
}

func TestSecretTypes(t *testing.T) {
	sts := builtin.SecretTypes()
	require.NotEmpty(t, sts)

	t.Run("file", func(t *testing.T) {
		st, ok := sts["file"]
		require.True(t, ok)
		assert.Equal(t, "file", st.Name)
	})

	t.Run("vault", func(t *testing.T) {
		st, ok := sts["vault"]
		require.True(t, ok)
		assert.Equal(t, "vault", st.Name)
	})
}

func TestSecretTypeHCL(t *testing.T) {
	t.Run("existing file", func(t *testing.T) {
		data, ok := builtin.SecretTypeHCL("file")
		assert.True(t, ok)
		assert.NotEmpty(t, data)
	})

	t.Run("existing vault", func(t *testing.T) {
		data, ok := builtin.SecretTypeHCL("vault")
		assert.True(t, ok)
		assert.NotEmpty(t, data)
	})

	t.Run("nonexistent", func(t *testing.T) {
		_, ok := builtin.SecretTypeHCL("nonexistent")
		assert.False(t, ok)
	})
}

func TestNotificationTypes(t *testing.T) {
	nts := builtin.NotificationTypes()
	assert.Nil(t, nts, "no built-in notification types are embedded")
}

func TestNotificationTypeHCL(t *testing.T) {
	data, ok := builtin.NotificationTypeHCL("slack")
	assert.False(t, ok)
	assert.Nil(t, data)

	data, ok = builtin.NotificationTypeHCL("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, data)
}

func TestRunnerBlockParsedOnDomainTypes(t *testing.T) {
	t.Run("resource_type", func(t *testing.T) {
		hcl := []byte(`
resource_type "test" {
  check "exec" {
    path = "/bin/true"
  }
  runner "docker" {}
}
`)
		var wrapper struct {
			ResourceTypes []restype.ResourceType `hcl:"resource_type,block"`
		}
		err := hclsimple.Decode("test.hcl", hcl, nil, &wrapper)
		require.NoError(t, err)
		require.Len(t, wrapper.ResourceTypes, 1)
		rt := wrapper.ResourceTypes[0]
		require.NotNil(t, rt.Runner)
		assert.Equal(t, "docker", rt.Runner.Runner)
	})

	t.Run("secret_type", func(t *testing.T) {
		hcl := []byte(`
secret_type "test" {
  get "exec" {
    path = "/bin/true"
  }
  runner "docker" {}
}
`)
		var wrapper struct {
			SecretTypes []sectype.SecretType `hcl:"secret_type,block"`
		}
		err := hclsimple.Decode("test.hcl", hcl, nil, &wrapper)
		require.NoError(t, err)
		require.Len(t, wrapper.SecretTypes, 1)
		st := wrapper.SecretTypes[0]
		require.NotNil(t, st.Runner)
		assert.Equal(t, "docker", st.Runner.Runner)
	})

	t.Run("notification_type", func(t *testing.T) {
		hcl := []byte(`
notification_type "test" {
  notify "exec" {
    path = "/bin/true"
  }
  runner "docker" {}
}
`)
		var wrapper struct {
			NotificationTypes []notiftype.NotificationType `hcl:"notification_type,block"`
		}
		err := hclsimple.Decode("test.hcl", hcl, nil, &wrapper)
		require.NoError(t, err)
		require.Len(t, wrapper.NotificationTypes, 1)
		nt := wrapper.NotificationTypes[0]
		require.NotNil(t, nt.Runner)
		assert.Equal(t, "docker", nt.Runner.Runner)
	})
}

func TestServiceHCL(t *testing.T) {
	data, ok := builtin.ServiceHCL("postgres")
	assert.False(t, ok)
	assert.Nil(t, data)

	data, ok = builtin.ServiceHCL("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, data)
}

func TestRunnerHCL(t *testing.T) {
	t.Run("existing", func(t *testing.T) {
		data, ok := builtin.RunnerHCL("exec")
		assert.True(t, ok)
		assert.NotEmpty(t, data)
	})

	t.Run("nonexistent", func(t *testing.T) {
		_, ok := builtin.RunnerHCL("nonexistent")
		assert.False(t, ok)
	})
}
