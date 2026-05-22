package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xescugc/pikoci/pikoci/mock"
	"github.com/xescugc/pikoci/pikoci/user"
	"go.uber.org/mock/gomock"
)

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
					map[string]interface{}{"team_canonical": "main", "admin": false},
				},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Admin: false}},
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
				Memberships: []user.Member{{TeamCanonical: "main", Admin: false}},
			},
			expected: true,
		},
		{
			name: "membership removed",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "admin": false},
				},
			},
			dbUser: &user.WithMemberships{
				User: user.User{Admin: false},
			},
			expected: true,
		},
		{
			name: "membership admin changed",
			jwtUser: map[string]interface{}{
				"admin": false,
				"memberships": []interface{}{
					map[string]interface{}{"team_canonical": "main", "admin": false},
				},
			},
			dbUser: &user.WithMemberships{
				User:        user.User{Admin: false},
				Memberships: []user.Member{{TeamCanonical: "main", Admin: true}},
			},
			expected: true,
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

func TestRefreshTokenEndpoint(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)
	secret := []byte("test-secret")
	logger := slog.Default()

	handler := Handler(s, secret, logger)
	server := httptest.NewServer(handler)
	defer server.Close()

	um := &user.WithMemberships{
		User:        user.User{Username: "pepito"},
		Memberships: []user.Member{},
	}
	jwtToken := signJWT(t, secret, um)

	updatedUM := &user.WithMemberships{
		User:        user.User{Username: "pepito"},
		Memberships: []user.Member{{TeamCanonical: "main", Admin: false}},
	}

	// Stale-check in middleware calls GetUser; RefreshToken is the handler
	s.EXPECT().GetUser(gomock.Any(), "pepito").Return(updatedUM, nil)
	s.EXPECT().RefreshToken(gomock.Any(), "pepito").Return(updatedUM, signJWT(t, secret, updatedUM), nil)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/refresh-token.json", nil)
	require.NoError(t, err)
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

func TestXRefreshTokenHeader(t *testing.T) {
	secret := []byte("test-secret")
	logger := slog.Default()

	t.Run("header set when memberships differ", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock.NewService(ctrl)
		handler := Handler(s, secret, logger)
		server := httptest.NewServer(handler)
		defer server.Close()

		um := &user.WithMemberships{
			User:        user.User{Username: "pepito"},
			Memberships: []user.Member{},
		}
		jwtToken := signJWT(t, secret, um)

		updatedUM := &user.WithMemberships{
			User:        user.User{Username: "pepito"},
			Memberships: []user.Member{{TeamCanonical: "main", Admin: false}},
		}

		// member() authz calls GetUser, then middleware stale-check calls GetUser again
		s.EXPECT().GetUser(gomock.Any(), "pepito").Return(updatedUM, nil).Times(2)
		s.EXPECT().ListTeams(gomock.Any(), "pepito").Return(nil, nil)

		req, err := http.NewRequest(http.MethodGet, server.URL+"/teams.json", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Equal(t, "true", resp.Header.Get("X-Refresh-Token"))
	})

	t.Run("header not set when memberships match", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock.NewService(ctrl)
		handler := Handler(s, secret, logger)
		server := httptest.NewServer(handler)
		defer server.Close()

		um := &user.WithMemberships{
			User:        user.User{Username: "pepito"},
			Memberships: []user.Member{{TeamCanonical: "main", Admin: false}},
		}
		jwtToken := signJWT(t, secret, um)

		s.EXPECT().GetUser(gomock.Any(), "pepito").Return(um, nil).Times(2)
		s.EXPECT().ListTeams(gomock.Any(), "pepito").Return(nil, nil)

		req, err := http.NewRequest(http.MethodGet, server.URL+"/teams.json", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+jwtToken)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Empty(t, resp.Header.Get("X-Refresh-Token"))
	})

	t.Run("header not set for worker tokens", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		s := mock.NewService(ctrl)
		handler := Handler(s, secret, logger)
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
		req, err := http.NewRequest(http.MethodGet, server.URL+"/teams.json", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+workerJWT)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Empty(t, resp.Header.Get("X-Refresh-Token"))
	})
}
