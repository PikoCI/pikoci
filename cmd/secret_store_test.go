package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSecretValue_ValueFlagRefusesSecret(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := readSecretValue(cmd, "TOKEN", "ghp_from_flag", false, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to read a secret from the command line")
}

func TestReadSecretValue_ValueFlagAllowsPlain(t *testing.T) {
	cmd := &cobra.Command{}
	v, err := readSecretValue(cmd, "LABEL", "staging", false, false)
	require.NoError(t, err)
	assert.Equal(t, "staging", v)
}

func TestReadSecretValue_ValueAndStdinMutuallyExclusive(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := readSecretValue(cmd, "TOKEN", "x", true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestReadLine_PreservesSpaces(t *testing.T) {
	got, err := readLine(strings.NewReader("staging server\n"))
	require.NoError(t, err)
	assert.Equal(t, "staging server", got)
}

func TestReadLine_NoTrailingNewline(t *testing.T) {
	got, err := readLine(strings.NewReader("staging server"))
	require.NoError(t, err)
	assert.Equal(t, "staging server", got)
}

func TestReadLine_TrimsCarriageReturn(t *testing.T) {
	got, err := readLine(strings.NewReader("staging server\r\n"))
	require.NoError(t, err)
	assert.Equal(t, "staging server", got)
}
