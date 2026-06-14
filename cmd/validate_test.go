package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_Valid(t *testing.T) {
	dir := t.TempDir()
	hcl := `
job "hello" {
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hi"]
    }
  }
}
`
	hclPath := filepath.Join(dir, "pipeline.hcl")
	require.NoError(t, os.WriteFile(hclPath, []byte(hcl), 0644))

	hclBytes, err := os.ReadFile(hclPath)
	require.NoError(t, err)

	_, err = pikoci.ReadPipeline(context.Background(), hclBytes, nil)
	assert.NoError(t, err)
}

func TestValidate_Invalid(t *testing.T) {
	_, err := pikoci.ReadPipeline(context.Background(), []byte("invalid!!!"), nil)
	assert.Error(t, err)
}

func TestValidate_WithVars(t *testing.T) {
	hcl := `
variable "msg" {
  type = string
}

job "hello" {
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["${var.msg}"]
    }
  }
}
`
	vars := map[string]interface{}{"msg": "hello"}
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), vars)
	assert.NoError(t, err)
}

func TestValidate_WithVarsFile(t *testing.T) {
	dir := t.TempDir()
	hcl := `
variable "msg" {
  type = string
}

job "hello" {
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["${var.msg}"]
    }
  }
}
`
	varsData := map[string]interface{}{"msg": "from-file"}
	varsBytes, err := json.Marshal(varsData)
	require.NoError(t, err)

	varsFile := filepath.Join(dir, "vars.json")
	require.NoError(t, os.WriteFile(varsFile, varsBytes, 0644))

	vars, err := buildVars(nil, varsFile)
	require.NoError(t, err)

	_, err = pikoci.ReadPipeline(context.Background(), []byte(hcl), vars)
	assert.NoError(t, err)
}

func TestValidate_MissingFile(t *testing.T) {
	_, err := os.ReadFile("/tmp/nonexistent-pikoci-test.hcl")
	assert.Error(t, err)
}

func TestReadPipeline_InvalidPassedConstraint(t *testing.T) {
	hcl := `
resource "git" "my-repo" {}
job "build" {
  get "git" "my-repo" {
    passed = ["nonexistent"]
  }
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown job "nonexistent" in passed`)
}

func TestReadPipeline_PassedForEachGroupValid(t *testing.T) {
	hcl := `
resource "git" "my-repo" {}
job "test" {
  for_each = {
    a = "val-a"
  }
  get "git" "my-repo" {}
  task "echo" {
    run "exec" { path = "echo" }
  }
}
job "deploy" {
  get "git" "my-repo" {
    passed = ["test"]
  }
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	assert.NoError(t, err)
}

func TestReadPipeline_InvalidGetResource(t *testing.T) {
	hcl := `
job "build" {
  get "git" "nonexistent" {}
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown resource")
}

func TestReadPipeline_InvalidPutResource(t *testing.T) {
	hcl := `
job "build" {
  task "echo" {
    run "exec" { path = "echo" }
  }
  put "git" "nonexistent" {}
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown resource")
}

func TestReadPipeline_InvalidTaskRunner(t *testing.T) {
	hcl := `
job "build" {
  task "echo" {
    run "nonexistent-runner" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `references unknown runner "nonexistent-runner"`)
}

func TestReadPipeline_InvalidNotifyStep(t *testing.T) {
	hcl := `
job "build" {
  task "echo" {
    run "exec" { path = "echo" }
  }
  notify "slack" "nonexistent" {}
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown notification")
}

func TestReadPipeline_InvalidResourceType(t *testing.T) {
	hcl := `
resource "nonexistent-type" "my-res" {}
job "build" {
  get "nonexistent-type" "my-res" {}
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `references unknown resource_type "nonexistent-type"`)
}

func TestReadPipeline_InvalidNotificationType(t *testing.T) {
	hcl := `
notification "nonexistent-type" "my-notif" {}
job "build" {
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `references unknown notification_type "nonexistent-type"`)
}

func TestReadPipeline_InvalidNotificationJobs(t *testing.T) {
	hcl := `
notification_type "slack" {
  notify "exec" {}
}
notification "slack" "my-notif" {
  on = ["success"]
  jobs = ["nonexistent"]
}
job "build" {
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `references unknown job "nonexistent"`)
}

func TestReadPipeline_BuiltinRunnerValid(t *testing.T) {
	hcl := `
job "build" {
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	assert.NoError(t, err)
}

func TestReadPipeline_MultipleErrors(t *testing.T) {
	hcl := `
resource "nonexistent-type" "my-res" {}
notification "nonexistent-notif-type" "my-notif" {}
job "build" {
  task "echo" {
    run "nonexistent-runner" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown resource_type")
	assert.Contains(t, err.Error(), "references unknown notification_type")
	assert.Contains(t, err.Error(), "references unknown runner")
}

func TestReadPipeline_PassedSpecificForEachInstance_Valid(t *testing.T) {
	hcl := `
resource "git" "my-repo" {}
job "test" {
  for_each = toset(["a", "b"])
  get "git" "my-repo" {}
  task "echo" {
    run "exec" { path = "echo" }
  }
}
job "deploy" {
  get "git" "my-repo" {
    passed = ["test--a"]
  }
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	assert.NoError(t, err)
}

func TestReadPipeline_PassedSpecificForEachInstance_Invalid(t *testing.T) {
	hcl := `
resource "git" "my-repo" {}
job "test" {
  for_each = toset(["a", "b"])
  get "git" "my-repo" {}
  task "echo" {
    run "exec" { path = "echo" }
  }
}
job "deploy" {
  get "git" "my-repo" {
    passed = ["test--c"]
  }
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown job "test--c" in passed`)
}

func TestReadPipeline_NotificationJobsSpecificForEachInstance_Valid(t *testing.T) {
	hcl := `
notification_type "slack" {
  notify "exec" {}
}
notification "slack" "my-notif" {
  on = ["success"]
  jobs = ["test--a"]
}
job "test" {
  for_each = toset(["a", "b"])
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	assert.NoError(t, err)
}

func TestReadPipeline_NotificationExcludeSpecificForEachInstance_Invalid(t *testing.T) {
	hcl := `
notification_type "slack" {
  notify "exec" {}
}
notification "slack" "my-notif" {
  on = ["success"]
  exclude = ["test--c"]
}
job "test" {
  for_each = toset(["a", "b"])
  task "echo" {
    run "exec" { path = "echo" }
  }
}
`
	_, err := pikoci.ReadPipeline(context.Background(), []byte(hcl), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown job "test--c"`)
}
