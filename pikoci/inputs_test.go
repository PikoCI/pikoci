package pikoci

import (
	"testing"

	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveInputValues(t *testing.T) {
	str := func(s string) *string { return &s }

	t.Run("no inputs returns nil", func(t *testing.T) {
		result, err := resolveInputValues(nil, nil, true)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("all provided", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "version", Type: "string"},
			{Name: "count", Type: "number"},
			{Name: "dry_run", Type: "bool"},
		}
		provided := map[string]string{
			"version": "v1.0",
			"count":   "5",
			"dry_run": "true",
		}
		result, err := resolveInputValues(inputs, provided, true)
		require.NoError(t, err)
		assert.Equal(t, "v1.0", result["version"])
		assert.Equal(t, "5", result["count"])
		assert.Equal(t, "true", result["dry_run"])
	})

	t.Run("defaults used when not provided", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "env", Type: "string", Default: str("staging")},
			{Name: "count", Type: "number", Default: str("1")},
		}
		result, err := resolveInputValues(inputs, nil, true)
		require.NoError(t, err)
		assert.Equal(t, "staging", result["env"])
		assert.Equal(t, "1", result["count"])
	})

	t.Run("required missing on manual trigger", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "version", Type: "string"},
		}
		_, err := resolveInputValues(inputs, nil, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required input")
	})

	t.Run("auto trigger uses zero values", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "version", Type: "string"},
			{Name: "count", Type: "number"},
			{Name: "dry_run", Type: "bool"},
		}
		result, err := resolveInputValues(inputs, nil, false)
		require.NoError(t, err)
		assert.Equal(t, "", result["version"])
		assert.Equal(t, "0", result["count"])
		assert.Equal(t, "false", result["dry_run"])
	})

	t.Run("unknown key rejected", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "version", Type: "string"},
		}
		_, err := resolveInputValues(inputs, map[string]string{"unknown": "val"}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown input")
	})

	t.Run("invalid number rejected", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "count", Type: "number"},
		}
		_, err := resolveInputValues(inputs, map[string]string{"count": "abc"}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid number")
	})

	t.Run("invalid bool rejected", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "flag", Type: "bool"},
		}
		_, err := resolveInputValues(inputs, map[string]string{"flag": "yes"}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be")
	})

	t.Run("options validated", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "env", Type: "string", Options: []string{"staging", "production"}},
		}
		_, err := resolveInputValues(inputs, map[string]string{"env": "dev"}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in options")
	})

	t.Run("options valid selection", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "env", Type: "string", Options: []string{"staging", "production"}, Default: str("staging")},
		}
		result, err := resolveInputValues(inputs, map[string]string{"env": "production"}, true)
		require.NoError(t, err)
		assert.Equal(t, "production", result["env"])
	})

	t.Run("multiple options validated", func(t *testing.T) {
		inputs := []job.Input{
			{Name: "regions", Type: "string", Options: []string{"us-east-1", "eu-west-1"}, Multiple: true},
		}
		result, err := resolveInputValues(inputs, map[string]string{"regions": "us-east-1,eu-west-1"}, true)
		require.NoError(t, err)
		assert.Equal(t, "us-east-1,eu-west-1", result["regions"])

		_, err = resolveInputValues(inputs, map[string]string{"regions": "us-east-1,invalid"}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in options")
	})
}
