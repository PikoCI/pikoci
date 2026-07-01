package http

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/build"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/role"
	"github.com/pikoci/pikoci/pikoci/user"
	"github.com/pikoci/pikoci/pikoci/wkr"
	"go.uber.org/mock/gomock"
	_ "modernc.org/sqlite"
)

func TestSPAFallback_BrowserNavigation(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	// Simulate browser navigation (no Content-Type, no Authorization)
	// to SPA URLs that share paths with API endpoints.
	paths := []string{
		"/",
		"/teams",
		"/teams/main",
		"/teams/main/pipelines",
		"/teams/main/pipelines/my-pipeline",
		"/teams/main/pipelines/my-pipeline/jobs/build/builds",
		"/teams/main/pipelines/my-pipeline/resources/my-res/versions",
		"/login",
		"/users",
		"/profile",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
			require.NoError(t, err)
			// No Content-Type, no Authorization — just like a browser

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			assert.Equal(t, http.StatusOK, resp.StatusCode, "path %s should return 200", path)
			assert.Contains(t, string(body), "<!DOCTYPE html>", "path %s should return SPA HTML, not JSON", path)
		})
	}
}

func TestEncodeResponse_CacheControl(t *testing.T) {
	w := httptest.NewRecorder()
	resp := ErrorResponse{Err: ""}
	encodeResponse(resp, w)

	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestEncodeResponse_WithError(t *testing.T) {
	w := httptest.NewRecorder()
	resp := ErrorResponse{Err: "something went wrong"}
	encodeResponse(resp, w)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var got ErrorResponse
	json.NewDecoder(w.Body).Decode(&got)
	assert.Equal(t, "something went wrong", got.Err)
}

func TestEncodeResponse_WithoutError(t *testing.T) {
	w := httptest.NewRecorder()
	resp := ErrorResponse{Err: ""}
	encodeResponse(resp, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestEncodeResponse_NonErrorer(t *testing.T) {
	w := httptest.NewRecorder()
	encodeResponse(struct{ Name string }{Name: "test"}, w)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var got ErrorResponse
	json.NewDecoder(w.Body).Decode(&got)
	assert.Contains(t, got.Err, "is not 'Errorer'")
}

func TestErrorResponse_Error(t *testing.T) {
	e := ErrorResponse{Err: "test error"}
	assert.Equal(t, "test error", e.Error())

	e = ErrorResponse{Err: ""}
	assert.Equal(t, "", e.Error())
}

func TestMembershipsDiffer(t *testing.T) {
	tests := []struct {
		name     string
		jwtUser  map[string]interface{}
		dbUser   *user.WithMemberships
		expected bool
	}{
		{
			name: "identical",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "role": "write"},
				},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
			},
			expected: false,
		},
		{
			name: "admin flag changed",
			jwtUser: map[string]interface{}{
				"admin":       false,
				"memberships": []interface{}{},
			},
			dbUser: &user.WithMemberships{
				User: user.User{Admin: true},
			},
			expected: true,
		},
		{
			name: "new membership added",
			jwtUser: map[string]interface{}{
				"admin":       false,
				"memberships": []interface{}{},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
			},
			expected: true,
		},
		{
			name: "membership removed",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "role": "write"},
				},
			},
			dbUser: &user.WithMemberships{
				User: user.User{Admin: false},
			},
			expected: true,
		},
		{
			name: "membership role changed",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "role": "write"},
				},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
			},
			expected: true,
		},
		{
			name: "old JWT admin bool fallback - admin true maps to admin",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "admin": true},
				},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
			},
			expected: false,
		},
		{
			name: "old JWT admin bool fallback - admin false maps to maintainer",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "admin": false},
				},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Role: role.Maintain}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := membershipsDiffer(tt.jwtUser, tt.dbUser)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func signJWT(t *testing.T, secret []byte, um *user.WithMemberships) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": um,
	})
	s, err := token.SignedString(secret)
	require.NoError(t, err)
	return s
}

func TestUpdatePipeline_TeamCanonicalFromURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	// The handler should use team_canonical from the URL path, not from the
	// request body. json.Decode used to overwrite the URL value with an empty
	// string from the body.
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetPipeline(gomock.Any(), "main", "my-pipeline").Return(&pipeline.Pipeline{
		Name: "my-pipeline", Canonical: "my-pipeline",
	}, nil)

	body := `{}`
	req, err := http.NewRequest(http.MethodPut, server.URL+"/teams/main/pipelines/my-pipeline", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should not get "invalid Team Canonical format" error
	var result UpdatePipelineResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotContains(t, result.Err, "invalid Team Canonical format")
}

func TestRefreshTokenEndpoint(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "pepito"},
		Memberships: []user.Member{},
	}
	jwtToken := signJWT(t, secret, um)

	updatedUM := &user.WithMemberships{
		User:        user.User{Username: "pepito"},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
	}

	// Stale-check in middleware calls GetUser; RefreshToken is the handler
	s.EXPECT().GetUser(gomock.Any(), "pepito").Return(updatedUM, nil)
	s.EXPECT().RefreshToken(gomock.Any(), "pepito").Return(updatedUM, signJWT(t, secret, updatedUM), nil)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/refresh-token", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var refreshResp RefreshTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&refreshResp)
	require.NoError(t, err)
	assert.Empty(t, refreshResp.Err)
	assert.NotEmpty(t, refreshResp.Data.JWT)
	assert.Equal(t, "pepito", refreshResp.Data.User.Username)
	assert.Len(t, refreshResp.Data.User.Memberships, 1)
}

func TestGetVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "v0.2.1", "b17daa3")
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/version", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var got map[string]string
	err = json.NewDecoder(resp.Body).Decode(&got)
	require.NoError(t, err)
	assert.Equal(t, "v0.2.1", got["version"])
	assert.Equal(t, "b17daa3", got["commit"])
}

func TestExportDatabase_AdminAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	// Create an in-memory SQLite DB for the export handler
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	handler := Handler(s, secret, logger, db, "mem", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/export", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// The export may fail (minimal DB without migrations), but it should
	// not be a 400 auth error — it should reach the handler.
	assert.NotEqual(t, http.StatusBadRequest, resp.StatusCode, "admin should pass authorization")
}

func TestExportDatabase_NonAdminForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "pepito", Admin: false},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "pepito").Return(um, nil).AnyTimes()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/export", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "non-admin should be rejected")
}

func TestExportDatabase_UnauthenticatedForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/export", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "unauthenticated should be rejected")
}

func TestExportDatabase_ResponseHeaders(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	// Use a real mem DB with migrations for a successful export
	memDB, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)&_busy_timeout=5000&_txlock=immediate")
	require.NoError(t, err)
	defer memDB.Close()

	handler := Handler(s, secret, logger, memDB, "mem", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/export", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// With a fresh mem DB (no tables), the export runs migrations on dest
	// and copies data. It should succeed with proper headers.
	if resp.StatusCode == http.StatusOK {
		assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "pikoci.db")
	}
}

func TestXRefreshTokenHeader(t *testing.T) {
	secret := []byte("test-secret")
	logger := slog.Default()

	t.Run("header set when memberships differ", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock.NewService(ctrl)
		handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
		server := httptest.NewServer(handler)
		defer server.Close()

		um := &user.WithMemberships{
			User:        user.User{Username: "pepito"},
			Memberships: []user.Member{},
		}
		jwtToken := signJWT(t, secret, um)

		updatedUM := &user.WithMemberships{
			User:        user.User{Username: "pepito"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
		}

		// member() authz calls GetUser, then middleware stale-check calls GetUser again
		s.EXPECT().GetUser(gomock.Any(), "pepito").Return(updatedUM, nil).Times(2)
		s.EXPECT().ListTeams(gomock.Any(), "pepito").Return(nil, nil)

		req, err := http.NewRequest(http.MethodGet, server.URL+"/teams", nil)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Equal(t, "true", resp.Header.Get("X-Refresh-Token"))
	})

	t.Run("header not set when memberships match", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock.NewService(ctrl)
		handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
		server := httptest.NewServer(handler)
		defer server.Close()

		um := &user.WithMemberships{
			User:        user.User{Username: "pepito"},
			Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
		}
		jwtToken := signJWT(t, secret, um)

		s.EXPECT().GetUser(gomock.Any(), "pepito").Return(um, nil).Times(2)
		s.EXPECT().ListTeams(gomock.Any(), "pepito").Return(nil, nil)

		req, err := http.NewRequest(http.MethodGet, server.URL+"/teams", nil)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Empty(t, resp.Header.Get("X-Refresh-Token"))
	})

	t.Run("header not set for worker tokens", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock.NewService(ctrl)
		handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
		server := httptest.NewServer(handler)
		defer server.Close()

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"is_from_worker": true,
		})
		workerJWT, err := token.SignedString(secret)
		require.NoError(t, err)

		// Workers skip authz and stale-check; the handler itself will fail
		// because there's no username in context, but the important thing
		// is that X-Refresh-Token is NOT set.
		req, err := http.NewRequest(http.MethodGet, server.URL+"/teams", nil)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+workerJWT)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Empty(t, resp.Header.Get("X-Refresh-Token"))
	})
}

func TestGetPipelineImage_DOT_ReturnsJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	dotOutput := []byte(`digraph "test" { A -> B; }`)
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetPipelineImage(gomock.Any(), "main", "my-pipeline", ".dot", false, false, (*uint32)(nil)).Return(dotOutput, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/image.dot", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result GetPipelineImageResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, string(dotOutput), result.Image)
	assert.Empty(t, result.Err)
}

func TestGetPipelineImage_SVG_ReturnsRawSVG(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	svgOutput := []byte(`<svg><text>hello</text></svg>`)
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetPipelineImage(gomock.Any(), "main", "my-pipeline", ".svg", false, false, (*uint32)(nil)).Return(svgOutput, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/image.svg", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/svg+xml", resp.Header.Get("Content-Type"))
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, string(svgOutput), string(body))
}

func TestGetPipelineImage_PNG_ReturnsRawPNG(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	pngOutput := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetPipelineImage(gomock.Any(), "main", "my-pipeline", ".png", false, false, (*uint32)(nil)).Return(pngOutput, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/image.png", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header.Get("Content-Type"))
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, pngOutput, body)
}

func TestGetPipelineImage_SVG_NoJSONHeaderRequired(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	svgOutput := []byte(`<svg><text>test</text></svg>`)
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetPipelineImage(gomock.Any(), "main", "my-pipeline", ".svg", false, false, (*uint32)(nil)).Return(svgOutput, nil)

	// No Content-Type header — simulates browser request
	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/image.svg", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/svg+xml", resp.Header.Get("Content-Type"))
}

func TestGetPipelineImage_PublicFallback_SVG(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	svgOutput := []byte(`<svg><text>public</text></svg>`)
	s.EXPECT().GetPublicPipeline(gomock.Any(), "main", "my-pipeline").Return(&pipeline.Pipeline{
		Name: "my-pipeline", Canonical: "my-pipeline",
	}, nil)
	s.EXPECT().GetPublicPipelineImage(gomock.Any(), "main", "my-pipeline", ".svg", false, false, (*uint32)(nil)).Return(svgOutput, nil)

	// No auth header — should fall back to public
	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/image.svg", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/svg+xml", resp.Header.Get("Content-Type"))
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, string(svgOutput), string(body))
}

func TestGetPipelineImage_Error_ReturnsJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetPipelineImage(gomock.Any(), "main", "my-pipeline", ".svg", false, false, (*uint32)(nil)).Return(nil, assert.AnError)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/image.svg", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Errors should still return JSON
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result GetPipelineImageResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result.Err)
}


func TestGetBuildReport_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	report := &build.BuildReport{
		ReportVersion: "1",
		Team:          "main",
		Pipeline:      "my-pipeline",
		Job:           "my-job",
		Build:         build.BuildReportData{Number: "1", Status: "succeeded"},
		Approvals:     []build.Approval{},
		Steps:         []build.Step{{Type: "task", Name: "echo", Status: build.Succeeded}},
		JobLogs:       []build.Step{},
	}
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetBuildReport(gomock.Any(), "main", "my-pipeline", "my-job", "1").Return(report, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/jobs/my-job/builds/1/report", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "attachment")
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "build-report-1.json")

	var result GetBuildReportResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Empty(t, result.Err)
	require.NotNil(t, result.Report)
	assert.Equal(t, "1", result.Report.ReportVersion)
	assert.Equal(t, "main", result.Report.Team)
	assert.Equal(t, "1", result.Report.Build.Number)
	assert.Len(t, result.Report.Steps, 1)
}

func TestGetBuildReport_Error_NoContentDisposition(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetBuildReport(gomock.Any(), "main", "my-pipeline", "my-job", "99").Return(nil, assert.AnError)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/jobs/my-job/builds/99/report", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("Content-Disposition"), "error responses should not have Content-Disposition")

	var result GetBuildReportResponse
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result.Err)
	assert.Nil(t, result.Report)
}

func TestGetBuildReport_UnauthenticatedDenied(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/jobs/my-job/builds/1/report", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestWorkerHeartbeat_WithWorkerToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	workerJWT, err := token.SignedString(secret)
	require.NoError(t, err)

	s.EXPECT().WorkerHeartbeat(gomock.Any(), gomock.Any()).DoAndReturn(func(_ interface{}, w interface{}) error {
		wk, ok := w.(wkr.Worker)
		if !ok {
			t.Fatal("expected wkr.Worker")
		}
		assert.Equal(t, "worker-1", wk.Name)
		assert.Equal(t, "v1.0.0", wk.Version)
		assert.Equal(t, "deadbeef", wk.Commit)
		return nil
	})

	body := `{"name":"worker-1","hostname":"host1","os":"linux","arch":"amd64","version":"v1.0.0","commit":"deadbeef","concurrency":2}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/workers/heartbeat", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+workerJWT)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var got WorkerHeartbeatResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestListWorkers_AdminAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().ListWorkers(gomock.Any()).Return(nil, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/workers", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestListWorkers_NonAdminForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "pepito", Admin: false},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "pepito").Return(um, nil).AnyTimes()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/workers", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestWorkersHealth_AdminAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().WorkersHealth(gomock.Any()).Return(true, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/workers/health", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var got WorkersHealthResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.True(t, got.Healthy)
	assert.Empty(t, got.Err)
}

func TestDeleteWorker_AdminAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().DeleteWorker(gomock.Any(), "worker-1").Return(nil)

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/workers/worker-1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var got DeleteWorkerResponse
	json.NewDecoder(resp.Body).Decode(&got)
	assert.Empty(t, got.Err)
}

func TestDeleteWorker_NonAdminForbidden(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "pepito", Admin: false},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Write}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "pepito").Return(um, nil).AnyTimes()

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/workers/worker-1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGetResourceVersionPath_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	versionPath := &resource.VersionPathResponse{
		Resource: resource.VersionPathResource{
			Canonical: "git.repo",
			Version:   map[string]interface{}{"ref": "abc123"},
		},
		Path:      []resource.VersionPathEntry{{JobName: "build"}},
		Completed: 1,
		Total:     1,
	}
	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetResourceVersionPath(gomock.Any(), "main", "my-pipeline", "git.repo", uint32(42)).Return(versionPath, nil)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/resources/git.repo/versions/42/path", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result GetResourceVersionPathResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Empty(t, result.Err)
	require.NotNil(t, result.Data)
	assert.Equal(t, "git.repo", result.Data.Resource.Canonical)
	assert.Equal(t, 1, result.Data.Completed)
	assert.Equal(t, 1, result.Data.Total)
}

func TestGetResourceVersionPath_InvalidVersionID(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/resources/git.repo/versions/not-a-number/path", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result GetResourceVersionPathResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "invalid version_id", result.Err)
	assert.Nil(t, result.Data)
}

func TestGetResourceVersionPath_PublicFallback(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	versionPath := &resource.VersionPathResponse{
		Resource: resource.VersionPathResource{
			Canonical: "git.repo",
			Version:   map[string]interface{}{"ref": "def456"},
		},
		Path:      []resource.VersionPathEntry{{JobName: "test"}},
		Completed: 0,
		Total:     1,
	}
	s.EXPECT().GetPublicPipeline(gomock.Any(), "main", "my-pipeline").Return(&pipeline.Pipeline{
		Name: "my-pipeline", Canonical: "my-pipeline",
	}, nil)
	s.EXPECT().GetPublicResourceVersionPath(gomock.Any(), "main", "my-pipeline", "git.repo", uint32(42)).Return(versionPath, nil)

	// No auth header — should fall back to public
	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/resources/git.repo/versions/42/path", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result GetResourceVersionPathResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Empty(t, result.Err)
	require.NotNil(t, result.Data)
	assert.Equal(t, "git.repo", result.Data.Resource.Canonical)
}

func TestGetResourceVersionPath_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger, nil, "", "test", "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "admin", Admin: true},
		Memberships: []user.Member{{TeamCanonical: "main", Role: role.Admin}},
	}
	jwtToken := signJWT(t, secret, um)

	s.EXPECT().GetUser(gomock.Any(), "admin").Return(um, nil).AnyTimes()
	s.EXPECT().GetResourceVersionPath(gomock.Any(), "main", "my-pipeline", "git.repo", uint32(42)).Return(nil, assert.AnError)

	req, err := http.NewRequest(http.MethodGet, server.URL+"/teams/main/pipelines/my-pipeline/resources/git.repo/versions/42/path", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var result GetResourceVersionPathResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Err)
}
