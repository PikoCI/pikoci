package wkr

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStaleThreshold(t *testing.T) {
	assert.Equal(t, 90*time.Second, StaleThreshold)
}

func TestStatusConstants(t *testing.T) {
	assert.Equal(t, Status("healthy"), StatusHealthy)
	assert.Equal(t, Status("stale"), StatusStale)
}

func TestComputeStatus_Healthy(t *testing.T) {
	now := time.Now()
	w := Worker{LastPingAt: now.Add(-30 * time.Second)}
	w.ComputeStatus(now)
	assert.Equal(t, StatusHealthy, w.Status)
}

func TestComputeStatus_Stale(t *testing.T) {
	now := time.Now()
	w := Worker{LastPingAt: now.Add(-2 * time.Minute)}
	w.ComputeStatus(now)
	assert.Equal(t, StatusStale, w.Status)
}

func TestComputeStatus_ExactThreshold(t *testing.T) {
	now := time.Now()
	w := Worker{LastPingAt: now.Add(-StaleThreshold)}
	w.ComputeStatus(now)
	// Exactly at threshold is not > threshold, so still healthy
	assert.Equal(t, StatusHealthy, w.Status)
}

func TestComputeStatus_JustOverThreshold(t *testing.T) {
	now := time.Now()
	w := Worker{LastPingAt: now.Add(-StaleThreshold - time.Millisecond)}
	w.ComputeStatus(now)
	assert.Equal(t, StatusStale, w.Status)
}
