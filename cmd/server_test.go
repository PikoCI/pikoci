package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDefaultCheckInterval(t *testing.T) {
	require.NoError(t, validateDefaultCheckInterval(""), "empty means the built-in default")
	require.NoError(t, validateDefaultCheckInterval("@every 2h"))
	require.NoError(t, validateDefaultCheckInterval("0 * * * *"))

	for _, spec := range []string{"6h", "@every 1s"} {
		err := validateDefaultCheckInterval(spec)
		require.Error(t, err, spec)
		assert.Contains(t, err.Error(), "--default-check-interval")
	}
}
