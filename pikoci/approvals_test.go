package pikoci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
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
	assert.Contains(t, err.Error(), "you have already voted on this build")
}

func TestApproveBuild_InvalidCanonical(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	err := s.S.ApproveBuild(ctx, "INVALID", "pp", "jn", "1", "alice", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestRejectBuild_NotWaiting(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.Pending,
	}, nil)

	err := s.S.RejectBuild(ctx, "main", "pp", "jn", "1", "bob", "reason")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not waiting for approval")
}

// Spec scenario 3: 1 of 2 approved, then rejected → Failed immediately
func TestRejectBuild_OverridesPartialApproval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Build has 1 approval already but needs 2 — a rejection should still fail it
	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Builds.EXPECT().CreateApproval(ctx, uint32(1), "bob", "rejected", "rollback risk").Return(nil)
	s.Builds.EXPECT().Update(ctx, "main", "pp", "jn", "1", gomock.Any()).Return(nil)

	err := s.S.RejectBuild(ctx, "main", "pp", "jn", "1", "bob", "rollback risk")
	require.NoError(t, err)
}

// Spec scenario 5: Retry of approved+failed build skips gate
func TestCreateJobBuild_WithApproveGate(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Job has an approval gate (no approve notify — notification will be skipped)
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{
		Name: "jn", ApproveLabel: "deploy to prod", ApproveCount: 1,
	}, nil)
	// FireApproveNotifications tries to re-read pipeline raw HCL
	s.Pipelines.EXPECT().Find(ctx, "main", "pp").Return(&pipeline.Pipeline{Name: "pp"}, nil)
	s.Builds.EXPECT().Create(ctx, "main", "pp", "jn", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _ string, b build.Build) (uint32, string, error) {
			// Verify the build was created with WaitingForApproval
			assert.Equal(t, build.WaitingForApproval, b.Status)
			return 1, "1", nil
		})

	b, err := s.S.CreateJobBuild(ctx, "main", "pp", "jn", build.Build{})
	require.NoError(t, err)
	assert.Equal(t, build.WaitingForApproval, b.Status)
}

// Spec scenario 5: Retry skips approval gate (hardcodes Pending)
func TestRetryJobBuild_SkipsApprovalGate(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Called twice: once in RetryJobBuild, once in CreateRetryJobBuild
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{Name: "jn"}, nil).Times(2)

	// Original build was rejected (failed)
	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.Failed,
	}, nil).Times(2) // Find called twice: once for retry check, once for parent

	// CreateRetryJobBuild uses CreateRetry (not Create) with Pending status
	s.Builds.EXPECT().CreateRetry(ctx, "main", "pp", "jn", "1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _, _, _ string, b build.Build) (uint32, string, error) {
			assert.Equal(t, build.Pending, b.Status, "retry should skip approval gate")
			return 2, "1.1", nil
		})

	err := s.S.RetryJobBuild(ctx, "main", "pp", "jn", "1")
	require.NoError(t, err)
}

// Test: CancelJobBuild on a waiting build
func TestCancelJobBuild_WaitingForApproval(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Builds.EXPECT().Update(ctx, "main", "pp", "jn", "1", gomock.Any()).Return(nil)
	s.Jobs.EXPECT().Find(ctx, "main", "pp", "jn").Return(&job.Job{Name: "jn"}, nil)
	s.Jobs.EXPECT().FindJobsBySerialGroups(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	err := s.S.CancelJobBuild(ctx, "main", "pp", "jn", "1")
	require.NoError(t, err)
}

// Test: GetJobBuild populates approvals for waiting builds
func TestGetJobBuild_PopulatesApprovals(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval,
	}, nil)
	s.Builds.EXPECT().FindApprovals(ctx, uint32(1)).Return([]build.Approval{
		{ID: 1, BuildID: 1, Username: "alice", Action: "approved", Message: "LGTM"},
	}, nil)
	s.Builds.EXPECT().FindGetVersions(ctx, uint32(1)).Return(nil, nil)

	b, err := s.S.GetJobBuild(ctx, "main", "pp", "jn", "1")
	require.NoError(t, err)
	require.Len(t, b.Approvals, 1)
	assert.Equal(t, "alice", b.Approvals[0].Username)
}

// Test: GetJobBuild does NOT populate approvals for non-waiting builds
func TestGetJobBuild_NoApprovalsForPending(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.Pending,
	}, nil)
	// FindApprovals is always called now, returns empty
	s.Builds.EXPECT().FindApprovals(ctx, uint32(1)).Return(nil, nil)
	s.Builds.EXPECT().FindGetVersions(ctx, uint32(1)).Return(nil, nil)

	b, err := s.S.GetJobBuild(ctx, "main", "pp", "jn", "1")
	require.NoError(t, err)
	assert.Nil(t, b.Approvals)
}

// Test: approve notification fires when build enters WaitingForApproval
func TestFireApproveNotifications(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	// Set up a test HTTP server to receive the webhook
	var mu sync.Mutex
	var receivedMethod string
	var receivedContentType string
	var receivedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Pipeline has a discord notification with the test server URL
	pp := &pipeline.Pipeline{
		Name: "pp",
		Notifications: []notification.Notification{
			{
				Type:      "discord",
				Name:      "alerts",
				Canonical: "discord.alerts",
				Params:    &notification.Params{Params: map[string]string{"webhook_url": ts.URL}},
			},
		},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "pp").Return(pp, nil)

	j := &job.Job{
		Name:         "deploy",
		ApproveLabel: "deploy to prod",
		ApproveCount: 1,
		ApproveNotify: []job.NotifyStep{
			{Type: "discord", Name: "alerts", Message: "Build #$BUILD_NUMBER needs approval"},
		},
	}

	s.P.FireApproveNotifications(ctx, "main", "pp", "deploy", j, "5")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "POST", receivedMethod)
	assert.Equal(t, "application/json", receivedContentType)
	require.NotNil(t, receivedBody)
	assert.Equal(t, "Build #5 needs approval", receivedBody["content"])
}

// Test: default notification message includes build URL when base_url is set
func TestFireApproveNotifications_DefaultMessageWithURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	var mu sync.Mutex
	var receivedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pp := &pipeline.Pipeline{
		Name: "pp",
		Notifications: []notification.Notification{
			{
				Type: "discord", Name: "alerts", Canonical: "discord.alerts",
				Params: &notification.Params{Params: map[string]string{
					"webhook_url": ts.URL,
					"base_url":    "https://ci.example.com",
				}},
			},
		},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "pp").Return(pp, nil)

	j := &job.Job{
		Name: "deploy", ApproveLabel: "deploy", ApproveCount: 1,
		ApproveNotify: []job.NotifyStep{{Type: "discord", Name: "alerts"}},
	}

	s.P.FireApproveNotifications(ctx, "main", "pp", "deploy", j, "3")

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, receivedBody)
	assert.Contains(t, receivedBody["content"], "Build #3 needs approval")
	assert.Contains(t, receivedBody["content"], "https://ci.example.com/teams/main/pipelines/pp/jobs/deploy/builds/3")
}

// Test: default notification message without base_url omits link
func TestFireApproveNotifications_DefaultMessageNoURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	var mu sync.Mutex
	var receivedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pp := &pipeline.Pipeline{
		Name: "pp",
		Notifications: []notification.Notification{
			{
				Type: "discord", Name: "alerts", Canonical: "discord.alerts",
				Params: &notification.Params{Params: map[string]string{"webhook_url": ts.URL}},
			},
		},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "pp").Return(pp, nil)

	j := &job.Job{
		Name: "deploy", ApproveLabel: "deploy", ApproveCount: 1,
		ApproveNotify: []job.NotifyStep{{Type: "discord", Name: "alerts"}},
	}

	s.P.FireApproveNotifications(ctx, "main", "pp", "deploy", j, "7")

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, receivedBody)
	assert.Equal(t, "⏳ [pp/deploy] Build #7 needs approval", receivedBody["content"])
	assert.NotContains(t, receivedBody["content"], "http")
}

// Test: notification skipped when no ApproveNotify and no raw HCL
func TestFireApproveNotifications_NoNotify(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Pipelines.EXPECT().Find(ctx, "main", "pp").Return(&pipeline.Pipeline{Name: "pp"}, nil)

	j := &job.Job{Name: "deploy", ApproveLabel: "deploy", ApproveCount: 1}

	// Should return without error — no webhook call
	s.P.FireApproveNotifications(ctx, "main", "pp", "deploy", j, "1")
}

// Test: $BUILD_URL variable interpolation
func TestFireApproveNotifications_BuildURLVariable(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	var mu sync.Mutex
	var receivedBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pp := &pipeline.Pipeline{
		Name: "pp",
		Notifications: []notification.Notification{
			{
				Type: "discord", Name: "alerts", Canonical: "discord.alerts",
				Params: &notification.Params{Params: map[string]string{
					"webhook_url": ts.URL,
					"base_url":    "https://ci.example.com/",
				}},
			},
		},
	}
	s.Pipelines.EXPECT().Find(ctx, "main", "pp").Return(pp, nil)

	j := &job.Job{
		Name: "deploy", ApproveLabel: "deploy", ApproveCount: 1,
		ApproveNotify: []job.NotifyStep{{Type: "discord", Name: "alerts", Message: "Approve at $BUILD_URL"}},
	}

	s.P.FireApproveNotifications(ctx, "main", "pp", "deploy", j, "10")

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, receivedBody)
	assert.Equal(t, "Approve at https://ci.example.com/teams/main/pipelines/pp/jobs/deploy/builds/10", receivedBody["content"])
}

// Test: CreateJobBuild with approval gate sets WaitingForApproval and
// populates version metadata on GetJobBuild
func TestGetJobBuild_WithPinnedVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := newService(ctrl)
	ctx := context.TODO()

	s.Builds.EXPECT().Find(ctx, "main", "pp", "jn", "1").Return(&build.Build{
		ID: 1, BuildNumber: "1", Status: build.WaitingForApproval, VersionID: 42,
	}, nil)
	s.Builds.EXPECT().FindApprovals(ctx, uint32(1)).Return(nil, nil)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(42)).Return(
		&resource.Version{ID: 42, Version: map[string]interface{}{"ref": "abc123"}}, "git.repo", nil,
	)
	s.Builds.EXPECT().FindGetVersions(ctx, uint32(1)).Return(
		map[string]uint32{"repo": 42}, nil,
	)
	s.Resources.EXPECT().FindVersionByID(ctx, uint32(42)).Return(
		&resource.Version{ID: 42, Version: map[string]interface{}{"ref": "abc123"}}, "git.repo", nil,
	)

	b, err := s.S.GetJobBuild(ctx, "main", "pp", "jn", "1")
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"ref": "abc123"}, b.VersionMetadata)
	require.NotNil(t, b.PinnedVersions)
	assert.Equal(t, map[string]interface{}{"ref": "abc123"}, b.PinnedVersions["git.repo"])
}

