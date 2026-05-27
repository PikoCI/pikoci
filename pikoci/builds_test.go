package pikoci_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"go.uber.org/mock/gomock"
	"gocloud.dev/pubsub"
)

func TestCreateJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "my-job", gomock.Any()).Return(uint32(1), "1", nil)

	b, err := s.S.CreateJobBuild(ctx, "main", "my-pipeline", "my-job", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(1), b.ID)
	assert.Equal(t, "1", b.BuildNumber)
	assert.Equal(t, build.Pending, b.Status)
}

func TestCreateJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateJobBuild(ctx, "INVALID", "my-pipeline", "my-job", build.Build{})
	require.Error(t, err)

	_, err = s.S.CreateJobBuild(ctx, "main", "INVALID", "my-job", build.Build{})
	require.Error(t, err)

	_, err = s.S.CreateJobBuild(ctx, "main", "my-pipeline", "INVALID", build.Build{})
	require.Error(t, err)
}

func TestListJobBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// limit=0 fetches all, DB returns DESC order
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(0)).Return([]*build.Build{
		{ID: 2, BuildNumber: "2", Status: build.Started},
		{ID: 1, BuildNumber: "1", Status: build.Succeeded},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, nil, 0)
	require.NoError(t, err)
	require.Len(t, builds, 2)
	assert.False(t, hasMore)
	// Newest first
	assert.Equal(t, uint32(2), builds[0].ID)
	assert.Equal(t, uint32(1), builds[1].ID)
}

func TestListJobBuilds_WithLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// DB returns limit+1 items (3) when we ask for limit=2 → hasMore=true
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(3)).Return([]*build.Build{
		{ID: 5, BuildNumber: "5"},
		{ID: 4, BuildNumber: "4"},
		{ID: 3, BuildNumber: "3"},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, nil, 2)
	require.NoError(t, err)
	require.Len(t, builds, 2)
	assert.True(t, hasMore)
	assert.Equal(t, uint32(5), builds[0].ID)
	assert.Equal(t, uint32(4), builds[1].ID)
}

func TestListJobBuilds_Before(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	before := uint32(4)
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", &before, (*uint32)(nil), uint32(3)).Return([]*build.Build{
		{ID: 3, BuildNumber: "3"},
		{ID: 2, BuildNumber: "2"},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", &before, nil, 2)
	require.NoError(t, err)
	require.Len(t, builds, 2)
	assert.False(t, hasMore)
}

func TestListJobBuilds_After(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	after := uint32(3)
	// DB returns ASC order for after queries
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), &after, uint32(0)).Return([]*build.Build{
		{ID: 4, BuildNumber: "4"},
		{ID: 5, BuildNumber: "5"},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, &after, 0)
	require.NoError(t, err)
	require.Len(t, builds, 2)
	assert.False(t, hasMore)
	// Should be reversed to newest-first
	assert.Equal(t, uint32(5), builds[0].ID)
	assert.Equal(t, uint32(4), builds[1].ID)
}

func TestUpdateJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").Return(&build.Build{Status: build.Started}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).Return(nil)

	err := s.S.UpdateJobBuild(ctx, "main", "my-pipeline", "my-job", "1", build.Build{Status: build.Succeeded})
	require.NoError(t, err)
}

func TestDeleteJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Delete(ctx, "main", "my-pipeline", "my-job", "1").Return(nil)

	err := s.S.DeleteJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.NoError(t, err)
}

func TestRetryJobBuild_BaseBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3").
		Return(&build.Build{ID: 5, BuildNumber: "3", Status: build.Succeeded}, nil)
	// RetryJobBuild now creates a pending build first
	s.Builds.EXPECT().CreateRetry(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		Return(uint32(10), "3.1", nil)
	s.Topic.EXPECT().Send(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, msg *pubsub.Message) error {
		assert.Contains(t, string(msg.Body), `"retry_build_number":"3"`)
		assert.Contains(t, string(msg.Body), `"retry_build_id":5`)
		assert.Contains(t, string(msg.Body), `"build_id":10`)
		return nil
	})

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3")
	require.NoError(t, err)
}

func TestRetryJobBuild_RetryOfRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Retrying "3.1" should extract parent "3" and look up that build's ID
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3.1").
		Return(&build.Build{ID: 7, BuildNumber: "3.1", Status: build.Failed}, nil)
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3").
		Return(&build.Build{ID: 5, BuildNumber: "3", Status: build.Succeeded}, nil)
	// RetryJobBuild now creates a pending build first
	s.Builds.EXPECT().CreateRetry(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		Return(uint32(12), "3.2", nil)
	s.Topic.EXPECT().Send(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, msg *pubsub.Message) error {
		assert.Contains(t, string(msg.Body), `"retry_build_number":"3"`)
		// Should use parent build ID (5), not the retry build ID (7)
		assert.Contains(t, string(msg.Body), `"retry_build_id":5`)
		assert.Contains(t, string(msg.Body), `"build_id":12`)
		return nil
	})

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3.1")
	require.NoError(t, err)
}

func TestRetryJobBuild_RunningBuildFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Started}, nil)

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still running or pending")
}

func TestCreateRetryJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().CreateRetry(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		Return(uint32(8), "3.1", nil)

	b, err := s.S.CreateRetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(8), b.ID)
	assert.Equal(t, "3.1", b.BuildNumber)
	assert.Equal(t, build.Pending, b.Status)
}

func TestStartPendingBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job", Concurrency: 2}, nil)
	s.Builds.EXPECT().CountRunning(ctx, "main", "my-pipeline", "my-job").Return(1, nil)
	s.Builds.EXPECT().StartPending(ctx, "main", "my-pipeline", "my-job", uint32(10)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(10)).Return(
		&build.Build{ID: 10, BuildNumber: "1", Status: build.Started}, nil)

	b, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "my-job", 10)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), b.ID)
}

func TestStartPendingBuild_ConcurrencyLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job", Concurrency: 1}, nil)
	s.Builds.EXPECT().CountRunning(ctx, "main", "my-pipeline", "my-job").Return(1, nil)

	_, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "my-job", 10)
	require.ErrorIs(t, err, pikoci.ErrConcurrencyLimit)
}

func TestFindOldestPendingBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &build.Build{ID: 5, BuildNumber: "5", Status: build.Pending}
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "my-job").Return(expected, nil)

	b, err := s.S.FindOldestPendingBuild(ctx, "main", "my-pipeline", "my-job")
	require.NoError(t, err)
	assert.Equal(t, expected, b)
}

func TestFindOldestPendingBuild_None(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "my-job").Return(nil, nil)

	b, err := s.S.FindOldestPendingBuild(ctx, "main", "my-pipeline", "my-job")
	require.NoError(t, err)
	assert.Nil(t, b)
}

func TestFindBuildGetVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := map[string]uint32{"my-repo": 42, "my-cron": 7}
	s.Builds.EXPECT().FindGetVersions(ctx, uint32(5)).Return(expected, nil)

	result, err := s.S.FindBuildGetVersions(ctx, "main", "my-pipeline", "my-job", 5)
	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestCancelJobBuild_Pending(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Pending}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).Return(nil)
	// Should also notify next pending build
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "my-job").
		Return(nil, nil)

	err := s.S.CancelJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.NoError(t, err)
}

func TestCancelJobBuild_Running_NotifiesNextPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Started}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).Return(nil)
	// Should check for next pending build
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "my-job").
		Return(nil, nil)

	err := s.S.CancelJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.NoError(t, err)
}
