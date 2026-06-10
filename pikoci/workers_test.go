package pikoci_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pikoci/pikoci/pikoci/wkr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestWorkerHeartbeat(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	w := wkr.Worker{
		Name:        "worker-1",
		Hostname:    "host1",
		OS:          "linux",
		Arch:        "amd64",
		GoVersion:   "go1.22",
		Version:     "v0.4.0",
		Concurrency: 2,
	}

	s.Workers.EXPECT().Upsert(ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, got wkr.Worker) error {
			assert.Equal(t, "worker-1", got.Name)
			assert.Equal(t, "host1", got.Hostname)
			assert.False(t, got.LastPingAt.IsZero(), "LastPingAt should be set")
			return nil
		},
	)

	err := s.S.WorkerHeartbeat(ctx, w)
	require.NoError(t, err)
}

func TestWorkerHeartbeat_UpsertError(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().Upsert(ctx, gomock.Any()).Return(fmt.Errorf("db error"))

	err := s.S.WorkerHeartbeat(ctx, wkr.Worker{Name: "w1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to upsert worker")
}

func TestListWorkers(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := []*wkr.Worker{
		{ID: 1, Name: "worker-1", Status: wkr.StatusHealthy},
		{ID: 2, Name: "worker-2", Status: wkr.StatusStale},
	}

	s.Workers.EXPECT().Filter(ctx).Return(expected, nil)

	workers, err := s.S.ListWorkers(ctx)
	require.NoError(t, err)
	assert.Len(t, workers, 2)
	assert.Equal(t, "worker-1", workers[0].Name)
	assert.Equal(t, "worker-2", workers[1].Name)
}

func TestListWorkers_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().Filter(ctx).Return(nil, fmt.Errorf("db error"))

	workers, err := s.S.ListWorkers(ctx)
	require.Error(t, err)
	assert.Nil(t, workers)
	assert.Contains(t, err.Error(), "failed to list workers")
}

func TestWorkersHealth_Healthy(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().Filter(ctx).Return([]*wkr.Worker{
		{Name: "w1", Status: wkr.StatusStale},
		{Name: "w2", Status: wkr.StatusHealthy},
	}, nil)

	healthy, err := s.S.WorkersHealth(ctx)
	require.NoError(t, err)
	assert.True(t, healthy)
}

func TestWorkersHealth_NoHealthyWorkers(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().Filter(ctx).Return([]*wkr.Worker{
		{Name: "w1", Status: wkr.StatusStale},
	}, nil)

	healthy, err := s.S.WorkersHealth(ctx)
	require.NoError(t, err)
	assert.False(t, healthy)
}

func TestWorkersHealth_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().Filter(ctx).Return(nil, nil)

	healthy, err := s.S.WorkersHealth(ctx)
	require.NoError(t, err)
	assert.False(t, healthy)
}

func TestWorkersHealth_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().Filter(ctx).Return(nil, fmt.Errorf("db error"))

	healthy, err := s.S.WorkersHealth(ctx)
	require.Error(t, err)
	assert.False(t, healthy)
	assert.Contains(t, err.Error(), "failed to check workers health")
}

func TestWorkerHeartbeat_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.WorkerHeartbeat(ctx, wkr.Worker{Name: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker name is required")
}

func TestDeleteWorker(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().DeleteByName(ctx, "worker-1").Return(nil)

	err := s.S.DeleteWorker(ctx, "worker-1")
	require.NoError(t, err)
}

func TestDeleteWorker_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Workers.EXPECT().DeleteByName(ctx, "missing").Return(fmt.Errorf("worker %q not found", "missing"))

	err := s.S.DeleteWorker(ctx, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteWorker_EmptyName(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.DeleteWorker(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worker name is required")
}

// Test that WorkerHeartbeat sets LastPingAt to approximately now
func TestWorkerHeartbeat_SetsLastPingAt(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	before := time.Now()
	s.Workers.EXPECT().Upsert(ctx, gomock.Any()).DoAndReturn(
		func(_ context.Context, got wkr.Worker) error {
			assert.True(t, !got.LastPingAt.Before(before), "LastPingAt should be >= test start time")
			assert.True(t, !got.LastPingAt.After(time.Now()), "LastPingAt should be <= now")
			return nil
		},
	)

	err := s.S.WorkerHeartbeat(ctx, wkr.Worker{Name: "w1"})
	require.NoError(t, err)
}
