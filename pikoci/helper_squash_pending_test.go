package pikoci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadPipeline_SquashPending(t *testing.T) {
	hcl := []byte(`
resource "cron" "timer" {
  check_interval = "@every 1m"
}

job "squashed" {
  squash_pending = true
  get "cron" "timer" { trigger = true }
  task "run" {
    run "exec" { path = "echo" }
  }
}

job "plain" {
  get "cron" "timer" { trigger = true }
  task "run" {
    run "exec" { path = "echo" }
  }
}
`)
	pp, err := ReadPipeline(context.Background(), hcl, nil)
	require.NoError(t, err)
	require.Len(t, pp.Jobs, 2)
	assert.True(t, pp.Jobs[0].SquashPending)
	assert.False(t, pp.Jobs[1].SquashPending)
}
