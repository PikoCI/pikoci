package pikoci

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebhookToken(t *testing.T) {
	t.Run("normal name", func(t *testing.T) {
		token, err := newWebhookToken("my-repo")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(token, "my-repo_"))
		assert.LessOrEqual(t, len(token), 255)
		// UUID is 36 chars, plus underscore and name
		assert.Equal(t, len("my-repo")+1+36, len(token))
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, err := newWebhookToken("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("very long name is truncated", func(t *testing.T) {
		longName := strings.Repeat("a", 300)
		token, err := newWebhookToken(longName)
		require.NoError(t, err)
		assert.Equal(t, 255, len(token))
		// UUID (36 chars) should be intact at the end
		parts := strings.SplitN(token, "_", 2)
		assert.Equal(t, 218, len(parts[0]))
		assert.Equal(t, 36, len(parts[1]))
	})
}
