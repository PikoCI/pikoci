package pikoci

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadPipeline_InputBlocks(t *testing.T) {
	hcl := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}

job "deploy" {
  input "version" {
    type        = "string"
    description = "Version to deploy"
    default     = "latest"
  }

  input "environment" {
    type    = "string"
    options = ["staging", "production"]
    default = "staging"
  }

  input "dry_run" {
    type    = "bool"
    default = "false"
  }

  input "regions" {
    type     = "string"
    options  = ["eu-west-1", "us-east-1"]
    multiple = true
  }

  get "cron" "timer" { trigger = true }
  task "deploy" {
    run "exec" { path = "echo" }
  }
}
`)
	pp, err := ReadPipeline(context.Background(), hcl, nil)
	require.NoError(t, err)
	require.Len(t, pp.Jobs, 1)

	j := pp.Jobs[0]
	require.Len(t, j.Inputs, 4)

	assert.Equal(t, "version", j.Inputs[0].Name)
	assert.Equal(t, "string", j.Inputs[0].Type)
	assert.Equal(t, "Version to deploy", j.Inputs[0].Description)
	require.NotNil(t, j.Inputs[0].Default)
	assert.Equal(t, "latest", *j.Inputs[0].Default)

	assert.Equal(t, "environment", j.Inputs[1].Name)
	assert.Equal(t, []string{"staging", "production"}, j.Inputs[1].Options)

	assert.Equal(t, "dry_run", j.Inputs[2].Name)
	assert.Equal(t, "bool", j.Inputs[2].Type)

	assert.Equal(t, "regions", j.Inputs[3].Name)
	assert.True(t, j.Inputs[3].Multiple)
}

func TestReadPipeline_InputValidation(t *testing.T) {
	base := func(inputBlock string) []byte {
		return []byte(`
resource "cron" "timer" {
  check_interval = "@every 1h"
}
job "test" {
` + inputBlock + `
  get "cron" "timer" {
    trigger = true
  }
  task "t" {
    run "exec" {
      path = "echo"
    }
  }
}
`)
	}

	t.Run("invalid type", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type = "invalid"
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid type")
	})

	t.Run("options with non-string type", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type    = "number"
    options = ["1", "2"]
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options can only be used with type string")
	})

	t.Run("multiple without options", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type     = "string"
    multiple = true
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multiple can only be used with options")
	})

	t.Run("default not in options", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type    = "string"
    options = ["a", "b"]
    default = "c"
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "default value")
	})

	t.Run("duplicate input names", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type = "string"
  }
  input "x" {
    type = "string"
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate input name")
	})

	t.Run("default does not match type", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type    = "number"
    default = "abc"
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid number")
	})

	t.Run("multi-select default not in options", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x" {
    type     = "string"
    options  = ["a", "b"]
    multiple = true
    default  = "a,c"
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not in options")
	})

	t.Run("input name is not a usable env var name", func(t *testing.T) {
		_, err := ReadPipeline(context.Background(), base(`
  input "x=y" {
    type = "string"
  }`), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "letters, digits and underscores")
	})

	t.Run("too many inputs", func(t *testing.T) {
		var inputs string
		for i := 0; i < 21; i++ {
			inputs += fmt.Sprintf(`  input "x%d" {
    type = "string"
  }
`, i)
		}
		_, err := ReadPipeline(context.Background(), base(inputs), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too many inputs")
	})
}
