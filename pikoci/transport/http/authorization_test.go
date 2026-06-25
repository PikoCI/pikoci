package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
	"go.uber.org/mock/gomock"
)

// TestRoleAuthorizationMatrix verifies that each role level is correctly
// allowed or denied access to representative routes from each tier.
func TestRoleAuthorizationMatrix(t *testing.T) {
	type testCase struct {
		name       string
		method     string
		path       string
		userRole   role.Role
		globalAdmin bool
		wantOK     bool // true = 200 (handler reached), false = 400 (auth denied)
	}

	tests := []testCase{
		// --- Viewer routes: viewer+ allowed ---
		{"viewer can list pipelines", http.MethodGet, "/teams/main/pipelines", role.Viewer, false, true},
		{"viewer can get team", http.MethodGet, "/teams/main", role.Viewer, false, true},

		// --- Operator routes: operator+ allowed, viewer denied ---
		{"operator can trigger job", http.MethodPost, "/teams/main/pipelines/p/jobs/j/trigger", role.Operator, false, true},
		{"operator can pause pipeline", http.MethodPost, "/teams/main/pipelines/p/pause", role.Operator, false, true},
		{"operator can cancel build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/cancel", role.Operator, false, true},
		{"operator can retry build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/retry", role.Operator, false, true},
		{"viewer denied trigger job", http.MethodPost, "/teams/main/pipelines/p/jobs/j/trigger", role.Viewer, false, false},
		{"viewer denied cancel build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/cancel", role.Viewer, false, false},
		{"viewer denied retry build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/retry", role.Viewer, false, false},
		{"viewer denied pause pipeline", http.MethodPost, "/teams/main/pipelines/p/pause", role.Viewer, false, false},
		{"viewer denied pin resource", http.MethodPost, "/teams/main/pipelines/p/resources/r/pin", role.Viewer, false, false},
		{"viewer denied trigger resource", http.MethodPost, "/teams/main/pipelines/p/resources/r/trigger", role.Viewer, false, false},

		// --- Maintainer routes: maintainer+ allowed, operator denied ---
		{"maintainer can create pipeline", http.MethodPost, "/teams/main/pipelines", role.Maintainer, false, true},
		{"operator denied create pipeline", http.MethodPost, "/teams/main/pipelines", role.Operator, false, false},

		// --- Admin routes: admin+ allowed, maintainer denied ---
		{"admin can create member", http.MethodPost, "/teams/main/members", role.Admin, false, true},
		{"admin can update team", http.MethodPut, "/teams/main", role.Admin, false, true},
		{"maintainer denied create member", http.MethodPost, "/teams/main/members", role.Maintainer, false, false},
		{"maintainer denied update team", http.MethodPut, "/teams/main", role.Maintainer, false, false},

		{"admin can delete team", http.MethodDelete, "/teams/main", role.Admin, false, true},
		{"maintainer denied delete team", http.MethodDelete, "/teams/main", role.Maintainer, false, false},

		// --- Global admin routes: only global admin ---
		{"global admin can list users", http.MethodGet, "/users", role.Admin, true, true},
		{"admin denied list users (no global admin)", http.MethodGet, "/users", role.Admin, false, false},
		{"global admin can create team", http.MethodPost, "/teams", role.Admin, true, true},
		{"global admin can list workers", http.MethodGet, "/workers", role.Admin, true, true},
		{"non-global-admin denied list workers", http.MethodGet, "/workers", role.Admin, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			svc := mock.NewService(ctrl)
			secret := []byte("test-secret")
			handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc")
			server := httptest.NewServer(handler)
			defer server.Close()

			um := &user.WithMemberships{
				User:        user.User{Username: "testuser", Admin: tt.globalAdmin},
				Memberships: []user.Member{{TeamCanonical: "main", Role: tt.userRole}},
			}
			svc.EXPECT().GetUser(gomock.Any(), "testuser").Return(um, nil).AnyTimes()

			// Stub service methods that handlers call after authorization passes.
			// We use AnyTimes() + permissive returns so the test only checks auth.
			svc.EXPECT().ListPipelines(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().GetTeam(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().TriggerPipelineJob(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().PausePipeline(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().UnpausePipeline(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().CancelJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().RetryJobBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().CreatePipeline(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().CreateTeamMember(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().UpdateTeam(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().DeleteTeam(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().ListUsers(gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().CreateTeam(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().ListWorkers(gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().PinResourceVersion(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().TriggerPipelineResource(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			// Sign JWT
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user": um})
			jwtStr, err := token.SignedString(secret)
			require.NoError(t, err)

			req, err := http.NewRequest(tt.method, server.URL+tt.path, strings.NewReader("{}"))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+jwtStr)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			if tt.wantOK {
				assert.Equal(t, http.StatusOK, resp.StatusCode, "expected 200 for %s", tt.name)
			} else {
				assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "expected 400 for %s", tt.name)
				var errResp ErrorResponse
				json.NewDecoder(resp.Body).Decode(&errResp)
				assert.NotEmpty(t, errResp.Err)
			}
		})
	}
}

// TestUnauthenticatedPublicPipelineAccess verifies that unauthenticated users
// can access public pipeline routes but are denied on non-public pipelines.
func TestUnauthenticatedPublicPipelineAccess(t *testing.T) {
	t.Run("public pipeline allows unauthenticated access", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc")
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "pub-pipe").Return(nil, nil).AnyTimes()
		svc.EXPECT().GetPipeline(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/pub-pipe", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("non-public pipeline denies unauthenticated access", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc")
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "priv-pipe").Return(nil, fmt.Errorf("not public")).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/priv-pipe", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// TestWorkerJWTBypassesAuthorization verifies that worker JWTs bypass role checks.
func TestWorkerJWTBypassesAuthorization(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	secret := []byte("test-secret")
	handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc")
	server := httptest.NewServer(handler)
	defer server.Close()

	// Stub the service method the handler calls after auth passes
	svc.EXPECT().StartPendingBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	// Sign a worker JWT (no user claim, just is_from_worker)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	jwtStr, err := token.SignedString(secret)
	require.NoError(t, err)

	// StartPendingBuild requires maintainer role, but worker JWT should bypass
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/teams/main/pipelines/p/jobs/j/builds/start-pending", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtStr)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "worker JWT should bypass authorization")
}
