package cmd

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunLocal_BasicExec(t *testing.T) {
	dir := t.TempDir()
	hcl := `
job "hello" {
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["Hello from local run!"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "hello", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunLocal_FailedJob(t *testing.T) {
	dir := t.TempDir()
	hcl := `
job "fail" {
  task "die" {
    run "exec" {
      path = "sh"
      args = ["-c", "exit 1"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "fail", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, exitCode)
}

func TestRunLocal_VarOverride(t *testing.T) {
	dir := t.TempDir()
	hcl := `
variable "greeting" {
  type    = string
  default = "default"
}

job "hello" {
  task "echo" {
    run "exec" {
      path = "sh"
      args = ["-c", "echo ${var.greeting}"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	vars := map[string]interface{}{"greeting": "overridden"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "hello", vars, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunLocal_VarsFile(t *testing.T) {
	dir := t.TempDir()
	hcl := `
variable "msg" {
  type    = string
  default = "none"
}

job "hello" {
  task "echo" {
    run "exec" {
      path = "sh"
      args = ["-c", "echo ${var.msg}"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	varsData := map[string]interface{}{"msg": "from-file"}
	varsBytes, err := json.Marshal(varsData)
	require.NoError(t, err)
	varsFile := filepath.Join(dir, "vars.json")
	require.NoError(t, os.WriteFile(varsFile, varsBytes, 0644))

	vars, err := buildVars(nil, varsFile)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "hello", vars, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunLocal_SecretVarOverride(t *testing.T) {
	dir := t.TempDir()
	hcl := `
secret_type "env" {
  source = "pikoci://file"
  format = "env"
  path   = "/nonexistent/secrets.env"
}

variable "my_secret" {
  type = string
  secret "env" {
    key = "MY_SECRET"
  }
}

job "hello" {
  task "echo" {
    run "exec" {
      path = "sh"
      args = ["-c", "echo ${var.my_secret}"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	// Override the secret-backed variable with --var so secret resolution is not needed
	vars := map[string]interface{}{"my_secret": "overridden-value"}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "hello", vars, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunLocal_ResourceOverride(t *testing.T) {
	dir := t.TempDir()

	// Create a local resource directory with a file
	resDir := filepath.Join(dir, "my-repo")
	require.NoError(t, os.MkdirAll(resDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(resDir, "hello.txt"), []byte("hello"), 0644))

	hcl := `
resource_type "git" {
  params = ["uri"]
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo [{\"ref\":\"abc\"}]"]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo pulling"]
  }
}

resource "git" "my-repo" {
  params {
    uri = "https://example.com/repo.git"
  }
}

job "test" {
  get "git" "my-repo" {
    trigger = true
  }
  task "check" {
    run "exec" {
      path = "sh"
      args = ["-c", "test -f my-repo/hello.txt"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	overrides := map[string]string{"git.my-repo": resDir}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "test", nil, overrides)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRunLocal_ResourceOverride_MissingPath(t *testing.T) {
	dir := t.TempDir()

	hcl := `
resource_type "git" {
  params = ["uri"]
  check "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo [{\"ref\":\"abc\"}]"]
  }
  pull "exec" {
    path = "/bin/sh"
    args = ["-ec", "echo pulling"]
  }
}

resource "git" "my-repo" {
  params {
    uri = "https://example.com/repo.git"
  }
}

job "test" {
  get "git" "my-repo" {
    trigger = true
  }
  task "check" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	overrides := map[string]string{"git.my-repo": filepath.Join(dir, "does-not-exist")}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	exitCode, err := runLocal(context.Background(), logger, hclPath, "test", nil, overrides)
	require.NoError(t, err)
	assert.Equal(t, 1, exitCode)
}

func TestRunLocal_InvalidJob(t *testing.T) {
	dir := t.TempDir()
	hcl := `
job "hello" {
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	_, err := runLocal(context.Background(), logger, hclPath, "nonexistent", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestRunLocal_InvalidPipeline(t *testing.T) {
	dir := t.TempDir()
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte("this is not valid HCL {{{"), 0644))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	_, err := runLocal(context.Background(), logger, hclPath, "test", nil, nil)
	require.Error(t, err)
}

func TestBuildVars(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		vars, err := buildVars(nil, "")
		require.NoError(t, err)
		assert.Nil(t, vars)
	})

	t.Run("flags only", func(t *testing.T) {
		vars, err := buildVars([]string{"key=value", "foo=bar"}, "")
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"key": "value", "foo": "bar"}, vars)
	})

	t.Run("invalid flag format", func(t *testing.T) {
		_, err := buildVars([]string{"no-equals"}, "")
		require.Error(t, err)
	})
}

func TestParseResourceFlags(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		overrides, err := parseResourceFlags(nil)
		require.NoError(t, err)
		assert.Nil(t, overrides)
	})

	t.Run("valid", func(t *testing.T) {
		overrides, err := parseResourceFlags([]string{"repo=/tmp/repo", "image=/tmp/image"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"repo": "/tmp/repo", "image": "/tmp/image"}, overrides)
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := parseResourceFlags([]string{"no-equals"})
		require.Error(t, err)
	})
}
