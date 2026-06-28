package pikoci_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"go.uber.org/mock/gomock"
)

func TestApproveBuild_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{
		Name: "jn", ApproveLabel: "deploy", ApproveCount: 1,
	}, nil)
	s.Builds.EXPECT().CreateApproval(ctx, uint32(1), "alice", "approved", "LGTM").Return(nil)
	s.Builds.EXPECT().CountApprovals(ctx, uint32(1)).Return(1, nil)
	s.Builds.EXPECT().Update(ctx, "main", "pp", "jn", "1", gomock.Any()).Return(nil)

	err := s.S.ApproveBuild(ctx, "main", "pp", "jn", "1", "alice", "LGTM")
	require.NoError(t, err)
}

func TestApproveBuild_MultiApproval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// First approval — not enough yet (needs 2)
	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{
		Name: "jn", ApproveLabel: "deploy", ApproveCount: 2,
	}, nil)
	s.Builds.EXPECT().CreateApproval(ctx, uint32(1), "alice", "approved", "").Return(nil)
	s.Builds.EXPECT().CountApprovals(ctx, uint32(1)).Return(1, nil)
	// No Update call — still waiting

	err := s.S.ApproveBuild(ctx, "main", "pp", "jn", "1", "alice", "")
	require.NoError(t, err)
}

func TestRejectBuild_ImmediatelyFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Builds.EXPECT().CreateApproval(ctx, uint32(1), "bob", "rejected", "not ready").Return(nil)
	s.Builds.EXPECT().Update(ctx, "main", "pp", "jn", "1", gomock.Any()).Return(nil)

	err := s.S.RejectBuild(ctx, "main", "pp", "jn", "1", "bob", "not ready")
	require.NoError(t, err)
}

func TestRejectBuild_RequiresMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.RejectBuild(ctx, "main", "pp", "jn", "1", "bob", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejection message is required")
}

func TestApproveBuild_NotWaiting(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.Pending,
	}, nil)

	err := s.S.ApproveBuild(ctx, "main", "pp", "jn", "1", "alice", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not waiting for approval")
}

func TestApproveBuild_NoApproveBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{
		Name: "jn",
	}, nil)

	err := s.S.ApproveBuild(ctx, "main", "pp", "jn", "1", "alice", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not have an approval gate")
}

func TestApproveBuild_DuplicateVote(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{
		Name: "jn", ApproveLabel: "deploy", ApproveCount: 1,
	}, nil)
	s.Builds.EXPECT().CreateApproval(ctx, uint32(1), "alice", "approved", "").Return(fmt.Errorf("UNIQUE constraint failed"))

	err := s.S.ApproveBuild(ctx, "main", "pp", "jn", "1", "alice", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to record approval")
}

func TestApproveBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.ApproveBuild(ctx, "INVALID", "pp", "jn", "1", "alice", "")
	require.Error(t, err)
}
