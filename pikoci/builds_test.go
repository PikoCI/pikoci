package pikoci_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"go.uber.org/mock/gomock"
)

func TestCreateJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job"}, nil)
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
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(0), ([]build.Status)(nil)).Return([]*build.Build{
		{ID: 2, BuildNumber: "2", Status: build.Started},
		{ID: 1, BuildNumber: "1", Status: build.Succeeded},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, nil, 0, nil)
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
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), (*uint32)(nil), uint32(3), ([]build.Status)(nil)).Return([]*build.Build{
		{ID: 5, BuildNumber: "5"},
		{ID: 4, BuildNumber: "4"},
		{ID: 3, BuildNumber: "3"},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, nil, 2, nil)
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
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", &before, (*uint32)(nil), uint32(3), ([]build.Status)(nil)).Return([]*build.Build{
		{ID: 3, BuildNumber: "3"},
		{ID: 2, BuildNumber: "2"},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", &before, nil, 2, nil)
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
	s.Builds.EXPECT().Filter(ctx, "main", "my-pipeline", "my-job", (*uint32)(nil), &after, uint32(0), ([]build.Status)(nil)).Return([]*build.Build{
		{ID: 4, BuildNumber: "4"},
		{ID: 5, BuildNumber: "5"},
	}, nil)

	builds, hasMore, err := s.S.ListJobBuilds(ctx, "main", "my-pipeline", "my-job", nil, &after, 0, nil)
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

	// Called twice: once in RetryJobBuild, once in CreateRetryJobBuild
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job"}, nil).Times(2)
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3").
		Return(&build.Build{ID: 5, BuildNumber: "3", Status: build.Succeeded}, nil).Times(2)
	// RetryJobBuild now creates a pending build with RetrySourceBuildID set
	s.Builds.EXPECT().CreateRetry(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pc, jn, pbn string, b build.Build) (uint32, string, error) {
			assert.Equal(t, uint32(5), b.RetrySourceBuildID)
			return uint32(10), "3.1", nil
		})

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3")
	require.NoError(t, err)
}

func TestRetryJobBuild_RetryOfRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Called twice: once in RetryJobBuild, once in CreateRetryJobBuild
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job"}, nil).Times(2)
	// Retrying "3.1" should extract parent "3"
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3.1").
		Return(&build.Build{ID: 7, BuildNumber: "3.1", Status: build.Failed}, nil)
	// Find parent build "3" to get its ID for version pinning
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3").
		Return(&build.Build{ID: 5, BuildNumber: "3", Status: build.Succeeded}, nil)
	// RetryJobBuild creates a pending build using parent build number
	s.Builds.EXPECT().CreateRetry(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		DoAndReturn(func(ctx context.Context, tc, pc, jn, pbn string, b build.Build) (uint32, string, error) {
			assert.Equal(t, uint32(5), b.RetrySourceBuildID)
			return uint32(12), "3.2", nil
		})

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3.1")
	require.NoError(t, err)
}

func TestRetryJobBuild_RunningBuildFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job"}, nil)
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Started}, nil)

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still running, pending, or waiting for approval")
}

func TestCreateRetryJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job"}, nil)
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
	// NotifySerialGroupPendingBuilds looks up the job
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job"}, nil)

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
	// NotifySerialGroupPendingBuilds looks up the job
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job"}, nil)

	err := s.S.CancelJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.NoError(t, err)
}

func TestReEnqueuePendingBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// ReEnqueuePendingBuilds now just calls Notify() — no DB queries needed
	err := s.P.ReEnqueuePendingBuilds(ctx)
	require.NoError(t, err)
}

func TestReEnqueuePendingBuilds_NoPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// ReEnqueuePendingBuilds now just calls Notify() — no DB queries needed
	err := s.P.ReEnqueuePendingBuilds(ctx)
	require.NoError(t, err)
}

func TestGetJobBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &build.Build{ID: 5, BuildNumber: "3", Status: build.Succeeded}
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3").Return(expected, nil)
	s.Builds.EXPECT().FindApprovals(ctx, uint32(5)).Return(nil, nil)
	s.Builds.EXPECT().FindGetVersions(ctx, uint32(5)).Return(nil, nil)

	b, err := s.S.GetJobBuild(ctx, "main", "my-pipeline", "my-job", "3")
	require.NoError(t, err)
	assert.Equal(t, expected, b)
}

func TestGetJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetJobBuild(ctx, "INVALID", "my-pipeline", "my-job", "1")
	require.Error(t, err)

	_, err = s.S.GetJobBuild(ctx, "main", "INVALID", "my-job", "1")
	require.Error(t, err)

	_, err = s.S.GetJobBuild(ctx, "main", "my-pipeline", "INVALID", "1")
	require.Error(t, err)
}

func TestGetJobBuild_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "99").Return(nil, assert.AnError)

	_, err := s.S.GetJobBuild(ctx, "main", "my-pipeline", "my-job", "99")
	require.Error(t, err)
}

func TestInsertBuildGetVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().InsertGetVersion(ctx, "main", "my-pipeline", "my-job", uint32(5), "my-repo", uint32(42)).Return(nil)

	err := s.S.InsertBuildGetVersion(ctx, "main", "my-pipeline", "my-job", 5, "my-repo", 42)
	require.NoError(t, err)
}

func TestGetBuildReport(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	expected := &build.Build{
		ID:          5,
		BuildNumber: "3",
		Status:      build.Succeeded,
		Steps:       []build.Step{{Type: "task", Name: "deploy", Status: build.Succeeded}},
		Job:         []build.Step{{Type: "job", Name: "summary", Status: build.Succeeded}},
		Approvals:   []build.Approval{{Username: "alice", Action: "approved"}},
	}
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "3").Return(expected, nil)
	s.Builds.EXPECT().FindApprovals(ctx, uint32(5)).Return(expected.Approvals, nil)
	s.Builds.EXPECT().FindGetVersions(ctx, uint32(5)).Return(nil, nil)

	report, err := s.S.GetBuildReport(ctx, "main", "my-pipeline", "my-job", "3")
	require.NoError(t, err)
	assert.Equal(t, "1", report.ReportVersion)
	assert.Equal(t, "main", report.Team)
	assert.Equal(t, "my-pipeline", report.Pipeline)
	assert.Equal(t, "my-job", report.Job)
	assert.Equal(t, "3", report.Build.Number)
	assert.Equal(t, "succeeded", report.Build.Status)
	assert.Len(t, report.Steps, 1)
	assert.Len(t, report.JobLogs, 1)
	assert.Len(t, report.Approvals, 1)
}

func TestGetBuildReport_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.GetBuildReport(ctx, "INVALID", "my-pipeline", "my-job", "1")
	require.Error(t, err)
}

func TestGetBuildReport_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "99").Return(nil, assert.AnError)

	_, err := s.S.GetBuildReport(ctx, "main", "my-pipeline", "my-job", "99")
	require.Error(t, err)
}

func TestReEnqueuePendingBuilds_NoPipelines(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// ReEnqueuePendingBuilds now just calls Notify() — no DB queries needed
	err := s.P.ReEnqueuePendingBuilds(ctx)
	require.NoError(t, err)
}

func TestCancelJobBuild_Running_NotifiesNextPending_WithPendingBuild(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Cancel a running build, Notify() is called instead of querying DB
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Started}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).Return(nil)
	// NotifySerialGroupPendingBuilds looks up the job
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job"}, nil)

	err := s.S.CancelJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.NoError(t, err)
}

func TestCancelJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.CancelJobBuild(ctx, "INVALID", "my-pipeline", "my-job", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Team Canonical")

	err = s.S.CancelJobBuild(ctx, "main", "INVALID", "my-job", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Pipeline Canonical")

	err = s.S.CancelJobBuild(ctx, "main", "my-pipeline", "INVALID", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Job Name")
}

func TestCancelJobBuild_AlreadyCompleted(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Succeeded}, nil)

	err := s.S.CancelJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running, pending, or waiting for approval")
}

func TestUpdateJobBuild_CancelledBuildNotOverwritten(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Build was cancelled; worker tries to update to succeeded — should stay cancelled
	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{Status: build.Cancelled}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).DoAndReturn(
		func(ctx context.Context, tc, pc, jn, bn string, b build.Build) error {
			assert.Equal(t, build.Cancelled, b.Status)
			return nil
		})

	err := s.S.UpdateJobBuild(ctx, "main", "my-pipeline", "my-job", "1", build.Build{Status: build.Succeeded})
	require.NoError(t, err)
}

func TestUpdateJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.UpdateJobBuild(ctx, "INVALID", "my-pipeline", "my-job", "1", build.Build{})
	require.Error(t, err)

	err = s.S.UpdateJobBuild(ctx, "main", "INVALID", "my-job", "1", build.Build{})
	require.Error(t, err)

	err = s.S.UpdateJobBuild(ctx, "main", "my-pipeline", "INVALID", "1", build.Build{})
	require.Error(t, err)
}

func TestDeleteJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.DeleteJobBuild(ctx, "INVALID", "my-pipeline", "my-job", "1")
	require.Error(t, err)

	err = s.S.DeleteJobBuild(ctx, "main", "INVALID", "my-job", "1")
	require.Error(t, err)

	err = s.S.DeleteJobBuild(ctx, "main", "my-pipeline", "INVALID", "1")
	require.Error(t, err)
}

func TestListJobBuilds_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, _, err := s.S.ListJobBuilds(ctx, "INVALID", "my-pipeline", "my-job", nil, nil, 0, nil)
	require.Error(t, err)

	_, _, err = s.S.ListJobBuilds(ctx, "main", "INVALID", "my-job", nil, nil, 0, nil)
	require.Error(t, err)

	_, _, err = s.S.ListJobBuilds(ctx, "main", "my-pipeline", "INVALID", nil, nil, 0, nil)
	require.Error(t, err)
}

func TestRetryJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.RetryJobBuild(ctx, "INVALID", "my-pipeline", "my-job", "1")
	require.Error(t, err)

	err = s.S.RetryJobBuild(ctx, "main", "INVALID", "my-job", "1")
	require.Error(t, err)

	err = s.S.RetryJobBuild(ctx, "main", "my-pipeline", "INVALID", "1")
	require.Error(t, err)
}

func TestStartPendingBuild_JobPaused(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job", Paused: true}, nil)

	_, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "my-job", 10)
	require.ErrorIs(t, err, pikoci.ErrJobPaused)
}

func TestStartPendingBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.StartPendingBuild(ctx, "INVALID", "my-pipeline", "my-job", 10)
	require.Error(t, err)

	_, err = s.S.StartPendingBuild(ctx, "main", "INVALID", "my-job", 10)
	require.Error(t, err)

	_, err = s.S.StartPendingBuild(ctx, "main", "my-pipeline", "INVALID", 10)
	require.Error(t, err)
}

func TestStartPendingBuild_NoConcurrencyLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Concurrency == 0 means no limit
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").Return(&job.Job{Name: "my-job", Concurrency: 0}, nil)
	s.Builds.EXPECT().StartPending(ctx, "main", "my-pipeline", "my-job", uint32(10)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(10)).Return(
		&build.Build{ID: 10, BuildNumber: "1", Status: build.Started}, nil)

	b, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "my-job", 10)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), b.ID)
}

func TestFindOldestPendingBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.FindOldestPendingBuild(ctx, "INVALID", "my-pipeline", "my-job")
	require.Error(t, err)

	_, err = s.S.FindOldestPendingBuild(ctx, "main", "INVALID", "my-job")
	require.Error(t, err)

	_, err = s.S.FindOldestPendingBuild(ctx, "main", "my-pipeline", "INVALID")
	require.Error(t, err)
}

func TestFindBuildGetVersions_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.FindBuildGetVersions(ctx, "INVALID", "my-pipeline", "my-job", 5)
	require.Error(t, err)

	_, err = s.S.FindBuildGetVersions(ctx, "main", "INVALID", "my-job", 5)
	require.Error(t, err)

	_, err = s.S.FindBuildGetVersions(ctx, "main", "my-pipeline", "INVALID", 5)
	require.Error(t, err)
}

// --- Serial Group tests ---

func TestStartPendingBuild_SerialGroupLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "deploy-staging").
		Return(&job.Job{Name: "deploy-staging", SerialGroups: []string{"deploy"}}, nil)
	s.Builds.EXPECT().CountRunningInSerialGroups(ctx, "main", "my-pipeline", []string{"deploy"}, "deploy-staging").
		Return(1, nil)

	_, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "deploy-staging", 10)
	require.ErrorIs(t, err, pikoci.ErrSerialGroupLimit)
}

func TestStartPendingBuild_SerialGroupNoRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "deploy-staging").
		Return(&job.Job{Name: "deploy-staging", SerialGroups: []string{"deploy"}}, nil)
	s.Builds.EXPECT().CountRunningInSerialGroups(ctx, "main", "my-pipeline", []string{"deploy"}, "deploy-staging").
		Return(0, nil)
	s.Builds.EXPECT().StartPending(ctx, "main", "my-pipeline", "deploy-staging", uint32(10)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(10)).Return(
		&build.Build{ID: 10, BuildNumber: "1", Status: build.Started}, nil)

	b, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "deploy-staging", 10)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), b.ID)
}

func TestStartPendingBuild_SerialGroupAndConcurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Concurrency allows but serial group blocks
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "deploy-staging").
		Return(&job.Job{Name: "deploy-staging", Concurrency: 2, SerialGroups: []string{"deploy"}}, nil)
	s.Builds.EXPECT().CountRunning(ctx, "main", "my-pipeline", "deploy-staging").Return(0, nil)
	s.Builds.EXPECT().CountRunningInSerialGroups(ctx, "main", "my-pipeline", []string{"deploy"}, "deploy-staging").
		Return(1, nil)

	_, err := s.S.StartPendingBuild(ctx, "main", "my-pipeline", "deploy-staging", 10)
	require.ErrorIs(t, err, pikoci.ErrSerialGroupLimit)
}

func TestNotifySerialGroupPendingBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "deploy-staging").
		Return(&job.Job{Name: "deploy-staging", SerialGroups: []string{"deploy"}}, nil)
	s.Jobs.EXPECT().FindJobsBySerialGroups(ctx, "main", "my-pipeline", []string{"deploy"}).
		Return([]*job.Job{
			{Name: "deploy-staging"},
			{Name: "deploy-prod"},
		}, nil)
	// sendPendingBuildNotification now just calls Notify() — no DB queries or topic sends

	s.P.NotifySerialGroupPendingBuilds(ctx, "main", "my-pipeline", "deploy-staging")
}

func TestNotifySerialGroupPendingBuilds_NoSerialGroups(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job"}, nil)
	// No further calls expected — job has no serial groups

	s.P.NotifySerialGroupPendingBuilds(ctx, "main", "my-pipeline", "my-job")
}

func TestCreateRetryJobBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	_, err := s.S.CreateRetryJobBuild(ctx, "INVALID", "my-pipeline", "my-job", "3", build.Build{})
	require.Error(t, err)

	_, err = s.S.CreateRetryJobBuild(ctx, "main", "INVALID", "my-job", "3", build.Build{})
	require.Error(t, err)

	_, err = s.S.CreateRetryJobBuild(ctx, "main", "my-pipeline", "INVALID", "3", build.Build{})
	require.Error(t, err)
}

func TestCountStartedBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().CountStarted(ctx).Return(3, nil)

	n, err := s.P.CountStartedBuilds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestRecoverOrphanedBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().FailStartedBuilds(ctx, "server shutdown: build was orphaned").Return(2, nil)

	n, err := s.P.RecoverOrphanedBuilds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestRecoverOrphanedBuilds_NoneOrphaned(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().FailStartedBuilds(ctx, "server shutdown: build was orphaned").Return(0, nil)

	n, err := s.P.RecoverOrphanedBuilds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// --- EvaluateDownstreamJobs tests ---

// makePipelineLintDeploy returns a pipeline with "lint" and "deploy" jobs.
// "deploy" has a get step on git.repo with passed=["lint"] and trigger=true.
func makePipelineLintDeploy() *pipeline.Pipeline {
	return &pipeline.Pipeline{
		Name: "my-pipeline",
		Jobs: []job.Job{
			{Name: "lint"},
			{
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"lint"},
							Trigger: true,
						},
					},
				},
			},
		},
	}
}

func TestEvaluateDownstreamJobs_TriggersWhenReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := makePipelineLintDeploy()
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	// FindReadyDownstreamVersion returns ready (version 42)
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(42), true, nil)

	// Resources.Find returns resource with no pinned version
	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{Type: "git", Name: "repo"}, nil)

	// FindOldestPending returns nil (no pending build)
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "deploy").
		Return(nil, nil)

	// Create is called to create a new pending build
	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "deploy", gomock.Any()).
		Return(uint32(10), "1", nil)
	// Versions are pinned at creation time for all builds
	s.Builds.EXPECT().InsertGetVersion(ctx, "main", "my-pipeline", "deploy", uint32(10), "repo", uint32(42)).Return(nil)

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_SkipsWhenNotReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := makePipelineLintDeploy()
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	// FindReadyDownstreamVersion returns not ready
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(0), false, nil)

	// Create must NOT be called — no expectation set; gomock will fail if it is

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_SkipsWhenPendingExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := makePipelineLintDeploy()
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(42), true, nil)

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{Type: "git", Name: "repo"}, nil)

	// FindOldestPending returns an existing pending build
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "deploy").
		Return(&build.Build{ID: 5, BuildNumber: "1", Status: build.Pending}, nil)

	// Create must NOT be called

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_WaitingApprovalBuildsPileUp(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Deploy job has an approval gate
	pp := &pipeline.Pipeline{
		Name: "my-pipeline",
		Jobs: []job.Job{
			{Name: "lint"},
			{
				Name:         "deploy",
				ApproveLabel: "deploy to prod",
				ApproveCount: 1,
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"lint"},
							Trigger: true,
						},
					},
				},
			},
		},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil).Times(2)

	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(99), true, nil)

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{Type: "git", Name: "repo"}, nil)

	// FindOldestPending returns nil (no pending build — the existing one is waiting_for_approval)
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "deploy").
		Return(nil, nil)

	// A new build IS created even though another waiting build may exist.
	// Waiting builds pile up — each version gets its own approval gate.
	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "deploy", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, b build.Build) (uint32, string, error) {
			assert.Equal(t, build.WaitingForApproval, b.Status, "downstream build with approve gate should be WaitingForApproval")
			assert.Equal(t, uint32(99), b.VersionID)
			return uint32(20), "2", nil
		})
	// Versions are pinned at creation time for approval builds
	s.Builds.EXPECT().InsertGetVersion(ctx, "main", "my-pipeline", "deploy", uint32(20), "repo", uint32(99)).Return(nil)

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_SkipsWhenResourcePinned(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := makePipelineLintDeploy()
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	// FindReadyDownstreamVersion returns version 42
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(42), true, nil)

	// Resource has PinnedVersionID=99 which differs from 42
	pinnedID := uint32(99)
	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{Type: "git", Name: "repo", PinnedVersionID: &pinnedID}, nil)

	// Create must NOT be called — resource pin mismatch

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_OnlyEvaluatesRelevantJobs(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Pipeline: lint, test, deploy (passed: ["lint"]), release (passed: ["test"])
	// Only deploy references "lint" so only deploy should be evaluated.
	pp := &pipeline.Pipeline{
		Name: "my-pipeline",
		Jobs: []job.Job{
			{Name: "lint"},
			{Name: "test"},
			{
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"lint"},
							Trigger: true,
						},
					},
				},
			},
			{
				Name: "release",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"test"},
							Trigger: true,
						},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	// Only deploy should be evaluated — FindReadyDownstreamVersion called for deploy only
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(0), false, nil)

	// release must NOT be evaluated (no expectation set for it)

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_MultiInputAllMustBeReady(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// deploy has two get steps:
	//   repo (passed: ["lint"], trigger: true)
	//   image (passed: ["build-image"], trigger: true)
	// Call with completedJobName="lint". First step ready, second not ready.
	// Create must NOT be called because ALL steps must be ready.
	pp := &pipeline.Pipeline{
		Name: "my-pipeline",
		Jobs: []job.Job{
			{Name: "lint"},
			{Name: "build-image"},
			{
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"lint"},
							Trigger: true,
						},
					},
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "docker",
							Name:    "image",
							Passed:  []string{"build-image"},
							Trigger: true,
						},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	// First step (repo) is ready
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(42), true, nil)

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{Type: "git", Name: "repo"}, nil)

	// Second step (image) is NOT ready
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"build-image"}, "deploy", "image", 1, (*uint32)(nil),
	).Return(uint32(0), false, nil)

	// Create must NOT be called — second step not ready

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}

func TestEvaluateDownstreamJobs_ForEachGroupExpansion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.Background()

	// "lint-go" and "lint-js" are for_each instances of group "lint".
	// "deploy" has passed: ["lint"] which should match both instances.
	// Completing "lint-go" should trigger evaluation of "deploy".
	pp := &pipeline.Pipeline{
		Canonical: "my-pipeline",
		Jobs: []job.Job{
			{Name: "lint-go", ForEachGroup: "lint"},
			{Name: "lint-js", ForEachGroup: "lint"},
			{
				Name: "deploy",
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"lint"},
							Trigger: true,
						},
					},
				},
			},
		},
	}

	s.Pipelines.EXPECT().Find(gomock.Any(), "main", "my-pipeline").Return(pp, nil)
	// Passed ["lint"] expands to ["lint-go", "lint-js"] via resolvePassedJobNames
	s.Builds.EXPECT().FindReadyDownstreamVersion(
		gomock.Any(), "main", "my-pipeline",
		[]string{"lint-go", "lint-js"}, "deploy", "repo", 2, (*uint32)(nil),
	).Return(uint32(42), true, nil)
	s.Resources.EXPECT().Find(gomock.Any(), "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{}, nil)
	s.Builds.EXPECT().FindOldestPending(gomock.Any(), "main", "my-pipeline", "deploy").
		Return(nil, nil)
	s.Builds.EXPECT().Create(gomock.Any(), "main", "my-pipeline", "deploy", gomock.Any()).
		Return(uint32(1), "1", nil)
	s.Builds.EXPECT().InsertGetVersion(gomock.Any(), "main", "my-pipeline", "deploy", uint32(1), "repo", uint32(42)).Return(nil)

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint-go")
	require.NoError(t, err)
}

// --- NextWork tests ---

func TestNextWork_RetryBuildSetsRetryFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FilterAll(ctx).Return([]*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{Canonical: "my-pipeline", Jobs: []job.Job{{Name: "my-job"}}},
			Team:     team.Team{Canonical: "main"},
		},
	}, nil)
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "my-job").
		Return(&build.Build{ID: 10, BuildNumber: "3.1", Status: build.Pending, RetrySourceBuildID: 5}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job"}, nil)
	s.Builds.EXPECT().StartPending(ctx, "main", "my-pipeline", "my-job", uint32(10)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(10)).Return(
		&build.Build{ID: 10, BuildNumber: "3.1", Status: build.Started, VersionID: 42, RetrySourceBuildID: 5}, nil)

	item, err := s.P.NextWork(ctx, workitem.WorkerContext{})
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "job", item.Type)
	assert.Equal(t, uint32(5), item.Body.RetryBuildID)
	assert.Equal(t, "3", item.Body.RetryBuildNumber)
	assert.Equal(t, uint32(10), item.Body.BuildID)
	assert.Equal(t, "3.1", item.Body.BuildNumber)
}

func TestNextWork_NonRetryBuildNoRetryFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().FilterAll(ctx).Return([]*pipeline.WithTeam{
		{
			Pipeline: pipeline.Pipeline{Canonical: "my-pipeline", Jobs: []job.Job{{Name: "my-job"}}},
			Team:     team.Team{Canonical: "main"},
		},
	}, nil)
	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "my-job").
		Return(&build.Build{ID: 7, BuildNumber: "1", Status: build.Pending}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job"}, nil)
	s.Builds.EXPECT().StartPending(ctx, "main", "my-pipeline", "my-job", uint32(7)).Return(nil)
	s.Builds.EXPECT().FindByID(ctx, uint32(7)).Return(
		&build.Build{ID: 7, BuildNumber: "1", Status: build.Started, VersionID: 10}, nil)

	item, err := s.P.NextWork(ctx, workitem.WorkerContext{})
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, "job", item.Type)
	assert.Equal(t, uint32(0), item.Body.RetryBuildID)
	assert.Equal(t, "", item.Body.RetryBuildNumber)
}

func TestRetryJobBuild_DisableRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job", DisableRetry: true}, nil)

	err := s.S.RetryJobBuild(ctx, "main", "my-pipeline", "my-job", "1")
	require.ErrorIs(t, err, pikoci.ErrRetryDisabled)
}

func TestCreateRetryJobBuild_DisableRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job", DisableRetry: true}, nil)

	_, err := s.S.CreateRetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3", build.Build{})
	require.ErrorIs(t, err, pikoci.ErrRetryDisabled)
}

func TestMarkBuildAsWarning_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Failed}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			assert.Equal(t, build.Warning, b.Status)
			return nil
		})

	// EvaluateDownstreamJobs needs pipeline
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(&pipeline.Pipeline{
		Name: "my-pipeline",
		Jobs: []job.Job{{Name: "my-job"}},
	}, nil)

	err := s.S.MarkBuildAsWarning(ctx, "main", "my-pipeline", "my-job", "1")
	require.NoError(t, err)
}

func TestMarkBuildAsWarning_NotFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "my-pipeline", "my-job", "1").
		Return(&build.Build{ID: 1, BuildNumber: "1", Status: build.Succeeded}, nil)

	err := s.S.MarkBuildAsWarning(ctx, "main", "my-pipeline", "my-job", "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not failed")
}

func TestMarkBuildAsWarning_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.MarkBuildAsWarning(ctx, "INVALID", "my-pipeline", "my-job", "1")
	require.Error(t, err)
}

func TestCreateJobBuild_Interruptible_CancelsOlderRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job", Interruptible: true}, nil)
	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "my-job", gomock.Any()).
		Return(uint32(5), "5", nil)
	// FindActiveBuilds returns an older running build
	s.Builds.EXPECT().FindActiveBuilds(ctx, "main", "my-pipeline", "my-job", uint32(5)).
		Return([]*build.Build{
			{ID: 3, BuildNumber: "3", Status: build.Started},
		}, nil)
	// The older build should be cancelled
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			assert.Equal(t, build.Cancelled, b.Status)
			assert.Equal(t, "superseded by newer build", b.Error)
			return nil
		})

	b, err := s.S.CreateJobBuild(ctx, "main", "my-pipeline", "my-job", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(5), b.ID)
}

func TestCreateJobBuild_Interruptible_CancelsPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job", Interruptible: true}, nil)
	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "my-job", gomock.Any()).
		Return(uint32(5), "5", nil)
	s.Builds.EXPECT().FindActiveBuilds(ctx, "main", "my-pipeline", "my-job", uint32(5)).
		Return([]*build.Build{
			{ID: 2, BuildNumber: "2", Status: build.Pending},
		}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "2", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			assert.Equal(t, build.Cancelled, b.Status)
			assert.Equal(t, "superseded by newer build", b.Error)
			return nil
		})

	b, err := s.S.CreateJobBuild(ctx, "main", "my-pipeline", "my-job", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(5), b.ID)
}

func TestCreateJobBuild_NotInterruptible_KeepsOlderBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Job is NOT interruptible
	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job", Interruptible: false}, nil)
	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "my-job", gomock.Any()).
		Return(uint32(5), "5", nil)
	// FindActiveBuilds should NOT be called

	b, err := s.S.CreateJobBuild(ctx, "main", "my-pipeline", "my-job", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(5), b.ID)
}

func TestCreateRetryJobBuild_Interruptible_CancelsOlder(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Jobs.EXPECT().Find(ctx, "main", "my-pipeline", "my-job").
		Return(&job.Job{Name: "my-job", Interruptible: true}, nil)
	s.Builds.EXPECT().CreateRetry(ctx, "main", "my-pipeline", "my-job", "3", gomock.Any()).
		Return(uint32(10), "3.1", nil)
	// FindActiveBuilds returns an older pending build
	s.Builds.EXPECT().FindActiveBuilds(ctx, "main", "my-pipeline", "my-job", uint32(10)).
		Return([]*build.Build{
			{ID: 4, BuildNumber: "4", Status: build.Pending},
		}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "my-job", "4", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			assert.Equal(t, build.Cancelled, b.Status)
			return nil
		})

	b, err := s.S.CreateRetryJobBuild(ctx, "main", "my-pipeline", "my-job", "3", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, uint32(10), b.ID)
	assert.Equal(t, "3.1", b.BuildNumber)
}

func TestEvaluateDownstreamJobs_Interruptible_CancelsOlderBuilds(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	pp := &pipeline.Pipeline{
		Name: "my-pipeline",
		Jobs: []job.Job{
			{Name: "lint"},
			{
				Name:          "deploy",
				Interruptible: true,
				Plan: []job.PlanStep{
					{
						Type: job.StepTypeGet,
						Get: &job.GetStep{
							Type:    "git",
							Name:    "repo",
							Passed:  []string{"lint"},
							Trigger: true,
						},
					},
				},
			},
		},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "my-pipeline").Return(pp, nil)

	s.Builds.EXPECT().FindReadyDownstreamVersion(
		ctx, "main", "my-pipeline",
		[]string{"lint"}, "deploy", "repo", 1, (*uint32)(nil),
	).Return(uint32(42), true, nil)

	s.Resources.EXPECT().Find(ctx, "main", "my-pipeline", "git.repo").
		Return(&resource.Resource{Type: "git", Name: "repo"}, nil)

	s.Builds.EXPECT().FindOldestPending(ctx, "main", "my-pipeline", "deploy").
		Return(nil, nil)

	s.Builds.EXPECT().Create(ctx, "main", "my-pipeline", "deploy", gomock.Any()).
		Return(uint32(10), "2", nil)
	s.Builds.EXPECT().InsertGetVersion(ctx, "main", "my-pipeline", "deploy", uint32(10), "repo", uint32(42)).Return(nil)

	// Interruptible: should cancel older running build
	s.Builds.EXPECT().FindActiveBuilds(ctx, "main", "my-pipeline", "deploy", uint32(10)).
		Return([]*build.Build{
			{ID: 5, BuildNumber: "1", Status: build.Started},
		}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "my-pipeline", "deploy", "1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) error {
			assert.Equal(t, build.Cancelled, b.Status)
			assert.Equal(t, "superseded by newer build", b.Error)
			return nil
		})

	err := s.S.EvaluateDownstreamJobs(ctx, "main", "my-pipeline", "lint")
	require.NoError(t, err)
}
