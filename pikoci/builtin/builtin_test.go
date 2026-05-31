package builtin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/builtin"
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
