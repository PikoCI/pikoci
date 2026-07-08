package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/apitoken"
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
		{"viewer can list pipelines", http.MethodGet, "/teams/main/pipelines", role.Read, false, true},
		{"viewer can get team", http.MethodGet, "/teams/main", role.Read, false, true},
		{"viewer can list audit log", http.MethodGet, "/teams/main/audit", role.Read, false, true},
		{"viewer can get build report", http.MethodGet, "/teams/main/pipelines/p/jobs/j/builds/1/report", role.Read, false, true},

		// --- Operator routes: operator+ allowed, viewer denied ---
		{"operator can trigger job", http.MethodPost, "/teams/main/pipelines/p/jobs/j/trigger", role.Write, false, true},
		{"operator can pause pipeline", http.MethodPost, "/teams/main/pipelines/p/pause", role.Write, false, true},
		{"operator can cancel build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/cancel", role.Write, false, true},
		{"operator can retry build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/retry", role.Write, false, true},
		{"viewer denied trigger job", http.MethodPost, "/teams/main/pipelines/p/jobs/j/trigger", role.Read, false, false},
		{"viewer denied cancel build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/cancel", role.Read, false, false},
		{"viewer denied retry build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/retry", role.Read, false, false},
		{"viewer denied pause pipeline", http.MethodPost, "/teams/main/pipelines/p/pause", role.Read, false, false},
		{"viewer denied pin resource", http.MethodPost, "/teams/main/pipelines/p/resources/r/pin", role.Read, false, false},
		{"viewer denied trigger resource", http.MethodPost, "/teams/main/pipelines/p/resources/r/trigger", role.Read, false, false},

		// --- Maintainer routes: maintainer+ allowed, operator denied ---
		{"maintainer can create pipeline", http.MethodPost, "/teams/main/pipelines", role.Maintain, false, true},
		{"operator denied create pipeline", http.MethodPost, "/teams/main/pipelines", role.Write, false, false},

		// --- Approval routes: maintain+ allowed, write denied ---
		{"maintain can approve build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/approve", role.Maintain, false, true},
		{"maintain can reject build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/reject", role.Maintain, false, true},
		{"write denied approve build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/approve", role.Write, false, false},
		{"write denied reject build", http.MethodPost, "/teams/main/pipelines/p/jobs/j/builds/1/reject", role.Write, false, false},

		// --- Admin routes: admin+ allowed, maintainer denied ---
		{"admin can create member", http.MethodPost, "/teams/main/members", role.Admin, false, true},
		{"admin can update team", http.MethodPut, "/teams/main", role.Admin, false, true},
		{"maintainer denied create member", http.MethodPost, "/teams/main/members", role.Maintain, false, false},
		{"maintainer denied update team", http.MethodPut, "/teams/main", role.Maintain, false, false},

		{"admin can delete team", http.MethodDelete, "/teams/main", role.Admin, false, true},
		{"maintainer denied delete team", http.MethodDelete, "/teams/main", role.Maintain, false, false},

		// --- Team worker token routes: admin only ---
		{"admin can generate team worker token", http.MethodPost, "/teams/main/worker-token", role.Admin, false, true},
		{"maintain denied generate team worker token", http.MethodPost, "/teams/main/worker-token", role.Maintain, false, false},
		{"admin can get team worker token", http.MethodGet, "/teams/main/worker-token", role.Admin, false, true},
		{"write denied get team worker token", http.MethodGet, "/teams/main/worker-token", role.Write, false, false},
		{"read denied get team worker token", http.MethodGet, "/teams/main/worker-token", role.Read, false, false},

		// --- Global admin routes: only global admin ---
		{"global admin can list users", http.MethodGet, "/users", role.Admin, true, true},
		{"admin denied list users (no global admin)", http.MethodGet, "/users", role.Admin, false, false},
		{"global admin can create team", http.MethodPost, "/teams", role.Admin, true, true},
		{"global admin can list workers", http.MethodGet, "/workers", role.Admin, true, true},
		{"non-global-admin denied list workers", http.MethodGet, "/workers", role.Admin, false, false},

		// --- OAuth admin routes: global admin only ---
		{"global admin can list oauth providers", http.MethodGet, "/admin/oauth-providers", role.Admin, true, true},
		{"non-global-admin denied list oauth providers", http.MethodGet, "/admin/oauth-providers", role.Admin, false, false},
		{"global admin can create oauth provider", http.MethodPost, "/admin/oauth-providers", role.Admin, true, true},
		{"non-global-admin denied create oauth provider", http.MethodPost, "/admin/oauth-providers", role.Admin, false, false},
		{"global admin can update oauth provider", http.MethodPut, "/admin/oauth-providers/github", role.Admin, true, true},
		{"non-global-admin denied update oauth provider", http.MethodPut, "/admin/oauth-providers/github", role.Admin, false, false},
		{"global admin can delete oauth provider", http.MethodDelete, "/admin/oauth-providers/github", role.Admin, true, true},
		{"non-global-admin denied delete oauth provider", http.MethodDelete, "/admin/oauth-providers/github", role.Admin, false, false},
		{"global admin can get auth settings", http.MethodGet, "/admin/auth-settings", role.Admin, true, true},
		{"non-global-admin denied get auth settings", http.MethodGet, "/admin/auth-settings", role.Admin, false, false},
		{"global admin can update auth settings", http.MethodPut, "/admin/auth-settings", role.Admin, true, true},
		{"non-global-admin denied update auth settings", http.MethodPut, "/admin/auth-settings", role.Admin, false, false},

		// --- Profile linked accounts: any authenticated user (role.Read) ---
		{"viewer can list linked accounts", http.MethodGet, "/profile/linked-accounts", role.Read, false, true},
		{"viewer can unlink account", http.MethodDelete, "/profile/linked-accounts/github", role.Read, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			svc := mock.NewService(ctrl)
			secret := []byte("test-secret")
			handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
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
		svc.EXPECT().ListAuditLog(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, false, nil).AnyTimes()
			svc.EXPECT().ApproveBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().RejectBuild(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().GetBuildReport(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().GenerateTeamWorkerToken(gomock.Any(), gomock.Any()).Return("token", nil).AnyTimes()
			svc.EXPECT().GetTeamWorkerToken(gomock.Any(), gomock.Any()).Return("token", nil).AnyTimes()
			svc.EXPECT().ListOAuthProviders(gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().CreateOAuthProvider(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().UpdateOAuthProvider(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().DeleteOAuthProvider(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().GetAuthSettings(gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().UpdateAuthSettings(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			svc.EXPECT().ListLinkedAccounts(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
			svc.EXPECT().UnlinkAccount(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			// Sign JWT
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user": um, "token_gen": um.TokenGen})
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
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
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
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
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
	handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
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

// apiTokenHash returns the SHA-256 hex hash of a plaintext API token.
func apiTokenHash(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// TestApiTokenAuth verifies that API tokens authenticate and authorize correctly.
func TestApiTokenAuth(t *testing.T) {
	plainToken := "pko_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash := apiTokenHash(plainToken)

	t.Run("personal token can access viewer route", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:  "admin",
			UserID:    1,
			UserAdmin: false,
			Personal:  true,
			TokenID:   1,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(1)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User:        user.User{Username: "admin"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Read}},
		}, nil)
		svc.EXPECT().ListPipelines(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("team-scoped token can access route on its team", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			Personal:      false,
			TeamCanonical: "main",
			TokenRole:     role.Write,
			TokenID:       2,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(2)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User:        user.User{Username: "admin"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
		}, nil)
		svc.EXPECT().ListPipelines(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("team-scoped token denied on different team", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			Personal:      false,
			TeamCanonical: "other-team",
			TokenRole:     role.Admin,
			TokenID:       3,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(3)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User: user.User{Username: "admin"},
			Memberships: []user.Member{
				{TeamCanonical: "main", Role: role.Admin},
				{TeamCanonical: "other-team", Role: role.Admin},
			},
		}, nil)

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("team-scoped token effective role is min of user and token role", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		// Token has operator role, user has admin — effective = operator
		// Creating a pipeline requires maintainer, so this should fail
		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			Personal:      false,
			TeamCanonical: "main",
			TokenRole:     role.Write,
			TokenID:       4,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(4)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User:        user.User{Username: "admin"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
		}, nil)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/teams/main/pipelines", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("user role downgraded — effective role follows user", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		// Token has maintainer role, but user has been downgraded to viewer
		// Effective = min(viewer, maintainer) = viewer — cannot create pipeline
		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			Personal:      false,
			TeamCanonical: "main",
			TokenRole:     role.Maintain,
			TokenID:       5,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(5)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User:        user.User{Username: "admin"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Read}},
		}, nil)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/teams/main/pipelines", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("team-scoped token denied on global admin route", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			UserAdmin:     true,
			Personal:      false,
			TeamCanonical: "main",
			TokenRole:     role.Admin,
			TokenID:       6,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(6)).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/users", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("personal token of global admin can access global admin route", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:  "admin",
			UserID:    1,
			UserAdmin: true,
			Personal:  true,
			TokenID:   7,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(7)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User: user.User{Username: "admin", Admin: true},
		}, nil)
		svc.EXPECT().ListUsers(gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/users", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("API token cannot manage tokens (jwtOnly)", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:  "admin",
			UserID:    1,
			UserAdmin: true,
			Personal:  true,
			TokenID:   8,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(8)).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api-tokens", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		pastTime := time.Now().Add(-1 * time.Hour)
		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:  "admin",
			UserID:    1,
			Personal:  true,
			TokenID:   9,
			ExpiresAt: &pastTime,
		}, nil)

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid token is rejected", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("invalid API token"))

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("team-scoped token denied on non-team route", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			Personal:      false,
			TeamCanonical: "main",
			TokenRole:     role.Admin,
			TokenID:       10,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(10)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User:        user.User{Username: "admin"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
		}, nil)

		// ChangePassword is a non-team route (tc=""), team-scoped tokens should be denied
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/users/change-password", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("no X-Refresh-Token header for API token requests", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:  "admin",
			UserID:    1,
			Personal:  true,
			TokenID:   11,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(11)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User:        user.User{Username: "admin"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Read}},
		}, nil)
		svc.EXPECT().ListPipelines(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Empty(t, resp.Header.Get("X-Refresh-Token"), "API token requests should not set X-Refresh-Token")
	})

	t.Run("team-scoped token on public-fallback route with wrong team is denied", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:      "admin",
			UserID:        1,
			Personal:      false,
			TeamCanonical: "other-team",
			TokenRole:     role.Admin,
			TokenID:       12,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(12)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
			User: user.User{Username: "admin"},
			Memberships: []user.Member{
				{TeamCanonical: "main", Role: role.Admin},
				{TeamCanonical: "other-team", Role: role.Admin},
			},
		}, nil)
		// requirePublicOrRole will fail, then middleware tries public fallback
		svc.EXPECT().GetPublicPipeline(gomock.Any(), "main", "some-pipe").Return(nil, fmt.Errorf("not public")).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/some-pipe", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

// TestMinRole verifies the minRole helper returns the lesser of two roles.
func TestMinRole(t *testing.T) {
	tests := []struct {
		a, b     role.Role
		expected role.Role
	}{
		{role.Admin, role.Read, role.Read},
		{role.Read, role.Admin, role.Read},
		{role.Write, role.Write, role.Write},
		{role.Maintain, role.Write, role.Write},
		{role.Read, role.Maintain, role.Read},
		{role.Admin, role.Admin, role.Admin},
	}

	for _, tt := range tests {
		t.Run(string(tt.a)+"_"+string(tt.b), func(t *testing.T) {
			assert.Equal(t, tt.expected, minRole(tt.a, tt.b))
		})
	}
}

// TestRoleOnTeam verifies the roleOnTeam helper.
func TestRoleOnTeam(t *testing.T) {
	um := &user.WithMemberships{
		User: user.User{Username: "testuser"},
		Memberships: []user.Member{
			{TeamCanonical: "team-a", Role: role.Admin},
			{TeamCanonical: "team-b", Role: role.Read},
		},
	}

	assert.Equal(t, role.Admin, roleOnTeam(um, "team-a"))
	assert.Equal(t, role.Read, roleOnTeam(um, "team-b"))
	assert.Equal(t, role.Role(""), roleOnTeam(um, "team-c")) // no membership
}

// TestApiTokenAuth_PersonalTokenMultiTeam verifies a personal token can access
// any team the user is a member of.
func TestApiTokenAuth_PersonalTokenMultiTeam(t *testing.T) {
	plainToken := "pko_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash := apiTokenHash(plainToken)

	setupPersonalToken := func(t *testing.T, svc *mock.Service) {
		svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
			Username:  "multi-user",
			UserID:    5,
			Personal:  true,
			TokenID:   20,
		}, nil)
		svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(20)).AnyTimes()
		svc.EXPECT().GetUser(gomock.Any(), "multi-user").Return(&user.WithMemberships{
			User: user.User{Username: "multi-user"},
			Memberships: []user.Member{
				{TeamCanonical: "team-a", Role: role.Admin},
				{TeamCanonical: "team-b", Role: role.Read},
			},
		}, nil)
	}

	t.Run("personal token accesses team-a", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		setupPersonalToken(t, svc)
		svc.EXPECT().ListPipelines(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/team-a/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "personal token should access team-a")
	})

	t.Run("personal token accesses team-b", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		setupPersonalToken(t, svc)
		svc.EXPECT().ListPipelines(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/team-b/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "personal token should access team-b")
	})

	t.Run("personal token denied on team user is not a member of", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		setupPersonalToken(t, svc)

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/team-c/pipelines", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "personal token should be denied on non-member team")
	})

	t.Run("personal token respects user role per team", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewService(ctrl)
		secret := []byte("test-secret")
		handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
		server := httptest.NewServer(handler)
		defer server.Close()

		// User is viewer on team-b — cannot create pipeline (requires maintainer)
		setupPersonalToken(t, svc)

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/teams/team-b/pipelines", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+plainToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "personal token should respect user's viewer role on team-b")
	})
}

// TestApiTokenAuth_UserRemovedFromTeam verifies that when a user is removed
// from a team, their personal token can no longer access that team.
func TestApiTokenAuth_UserRemovedFromTeam(t *testing.T) {
	plainToken := "pko_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash := apiTokenHash(plainToken)

	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	secret := []byte("test-secret")
	handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	// User WAS on team-a but has been removed — memberships no longer include team-a
	svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
		Username: "ex-member",
		UserID:   6,
		Personal: true,
		TokenID:  30,
	}, nil)
	svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(30)).AnyTimes()
	svc.EXPECT().GetUser(gomock.Any(), "ex-member").Return(&user.WithMemberships{
		User:        user.User{Username: "ex-member"},
		Memberships: []user.Member{}, // removed from all teams
	}, nil)

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/teams/team-a/pipelines", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plainToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "personal token should be denied after user removed from team")
}

// TestApiTokenAuth_TeamScopedTokenVariousRoutes verifies that a team-scoped
// token is consistently denied across different route types when targeting
// the wrong team.
func TestApiTokenAuth_TeamScopedTokenVariousRoutes(t *testing.T) {
	plainToken := "pko_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash := apiTokenHash(plainToken)

	routes := []struct {
		name   string
		method string
		path   string
	}{
		{"list pipelines", http.MethodGet, "/teams/wrong-team/pipelines"},
		{"trigger job", http.MethodPost, "/teams/wrong-team/pipelines/p/jobs/j/trigger"},
		{"create pipeline", http.MethodPost, "/teams/wrong-team/pipelines"},
		{"create member", http.MethodPost, "/teams/wrong-team/members"},
	}

	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			svc := mock.NewService(ctrl)
			secret := []byte("test-secret")
			handler := Handler(svc, secret, slog.Default(), nil, "", "test", "abc", "", nil)
			server := httptest.NewServer(handler)
			defer server.Close()

			// Token scoped to "my-team", trying to access "wrong-team"
			svc.EXPECT().FindApiTokenByHash(gomock.Any(), hash).Return(&apitoken.AuthResult{
				Username:      "admin",
				UserID:        1,
				Personal:      false,
				TeamCanonical: "my-team",
				TokenRole:     role.Admin,
				TokenID:       40,
			}, nil)
			svc.EXPECT().UpdateApiTokenLastUsed(gomock.Any(), uint32(40)).AnyTimes()
			svc.EXPECT().GetUser(gomock.Any(), "admin").Return(&user.WithMemberships{
				User: user.User{Username: "admin"},
				Memberships: []user.Member{
					{TeamCanonical: "my-team", Role: role.Admin},
					{TeamCanonical: "wrong-team", Role: role.Admin},
				},
			}, nil).AnyTimes()
			// For public fallback routes
			svc.EXPECT().GetPublicPipeline(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("not public")).AnyTimes()

			req, _ := http.NewRequest(rt.method, server.URL+rt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+plainToken)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
				"team-scoped token for my-team should be denied on %s to wrong-team", rt.name)
		})
	}
}
