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
