//go:build integration

package http_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/apitoken"
	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
)

func TestApiTokens_FullLifecycle(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	// --- Create personal token ---
	var personalToken apitoken.WithPlaintext
	t.Run("create personal token", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", adminJWT,
			`{"name":"test-personal","personal":true}`)
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.CreateApiTokenResponse
		decodeBody(t, resp, &got)
		require.Empty(t, got.Err)
		require.NotNil(t, got.Token)
		assert.Equal(t, "test-personal", got.Token.Name)
		assert.True(t, got.Token.Personal)
		assert.Contains(t, got.Token.Plaintext, "pko_")
		assert.Len(t, got.Token.Plaintext, 68)
		personalToken = *got.Token
	})

	// --- Create team-scoped token ---
	var teamToken apitoken.WithPlaintext
	t.Run("create team-scoped token", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", adminJWT,
			`{"name":"test-team","personal":false,"team_canonical":"main","role":"read"}`)
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.CreateApiTokenResponse
		decodeBody(t, resp, &got)
		require.Empty(t, got.Err)
		require.NotNil(t, got.Token)
		assert.False(t, got.Token.Personal)
		assert.Equal(t, "main", got.Token.TeamCanonical)
		assert.Equal(t, "read", string(got.Token.Role))
		teamToken = *got.Token
	})

	// --- List tokens ---
	t.Run("list tokens", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/api-tokens", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListApiTokensResponse
		decodeBody(t, resp, &got)
		require.Empty(t, got.Err)
		assert.GreaterOrEqual(t, len(got.Tokens), 2)
	})

	// --- Personal token can access any team ---
	t.Run("personal token accesses main team", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", personalToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// --- Personal token can access global admin routes ---
	t.Run("personal token accesses global admin route", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/users", personalToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// --- Team-scoped token can access its team ---
	t.Run("team-scoped token accesses its team", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", teamToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// --- Team-scoped token denied on global admin route ---
	t.Run("team-scoped token denied on global admin route", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/users", teamToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// --- Team-scoped viewer token denied on maintainer route ---
	t.Run("team-scoped viewer denied on maintainer route", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", teamToken.Plaintext,
			`{"name":"bad","config":""}`)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// --- API tokens cannot manage tokens ---
	t.Run("personal token cannot list tokens (jwtOnly)", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/api-tokens", personalToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("personal token cannot create tokens (jwtOnly)", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", personalToken.Plaintext,
			`{"name":"sneaky","personal":true}`)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// --- Invalid token rejected ---
	t.Run("invalid token rejected", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines",
			"pko_0000000000000000000000000000000000000000000000000000000000000000", "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// --- Duplicate name rejected ---
	t.Run("duplicate name rejected", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", adminJWT,
			`{"name":"test-personal","personal":true}`)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		var got thttp.CreateApiTokenResponse
		decodeBody(t, resp, &got)
		assert.Contains(t, got.Err, "test-personal")
	})

	// --- last_used_at updates ---
	t.Run("last_used_at updates after use", func(t *testing.T) {
		// Use the personal token
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", personalToken.Plaintext, "")
		resp.Body.Close()

		// List tokens and check last_used_at is set
		resp = doJSONRequest(t, http.MethodGet, pikoURL+"/api-tokens", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListApiTokensResponse
		decodeBody(t, resp, &got)
		require.Empty(t, got.Err)
		for _, tok := range got.Tokens {
			if tok.Name == "test-personal" {
				assert.NotNil(t, tok.LastUsedAt, "last_used_at should be set after token use")
			}
		}
	})

	// --- Delete token and verify it stops working ---
	t.Run("delete token revokes access", func(t *testing.T) {
		// Delete the team-scoped token
		resp := doJSONRequest(t, http.MethodDelete,
			pikoURL+"/api-tokens/"+itoa(teamToken.ID), adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.DeleteApiTokenResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)

		// Verify deleted token no longer works
		resp2 := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", teamToken.Plaintext, "")
		defer resp2.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
	})
}

func TestApiTokens_MembershipCleanup(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	// Create a user
	resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users", adminJWT,
		`{"username":"token-test-user","password":"pass123","full_name":"Token Test"}`)
	resp.Body.Close()

	// Add user to main team as operator
	resp = doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", adminJWT,
		`{"role":"write","user":{"username":"token-test-user"}}`)
	resp.Body.Close()

	// Login as the new user and create a team-scoped token
	userJWT := loginAndGetJWT(t, pikoURL, "token-test-user", "pass123")

	var userToken apitoken.WithPlaintext
	resp = doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", userJWT,
		`{"name":"member-token","personal":false,"team_canonical":"main","role":"read"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var createResp thttp.CreateApiTokenResponse
	decodeBody(t, resp, &createResp)
	resp.Body.Close()
	require.Empty(t, createResp.Err)
	userToken = *createResp.Token

	// Verify the token works
	t.Run("token works before removal", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", userToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Remove user from team
	resp = doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main/members/token-test-user", adminJWT, "")
	resp.Body.Close()
	requireOK(t, resp)

	// Verify the team-scoped token no longer works
	t.Run("team-scoped token revoked after member removal", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", userToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// Create a personal token for the user (should still work for teams they're on)
	// First re-add user to team so they can login
	resp = doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", adminJWT,
		`{"role":"read","user":{"username":"token-test-user"}}`)
	resp.Body.Close()

	userJWT = loginAndGetJWT(t, pikoURL, "token-test-user", "pass123")

	var personalToken apitoken.WithPlaintext
	resp = doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", userJWT,
		`{"name":"personal-survives","personal":true}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var createResp2 thttp.CreateApiTokenResponse
	decodeBody(t, resp, &createResp2)
	resp.Body.Close()
	require.Empty(t, createResp2.Err)
	personalToken = *createResp2.Token

	// Remove user from team again
	resp = doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main/members/token-test-user", adminJWT, "")
	resp.Body.Close()

	// Personal token should still exist (but fail on main team since user is no longer a member)
	t.Run("personal token survives member removal but loses team access", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", personalToken.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "personal token should lose access to team after removal")
	})
}

func TestApiTokens_RoleDowngrade(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	// Create a user with maintainer role
	resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users", adminJWT,
		`{"username":"downgrade-user","password":"pass123","full_name":"Downgrade User"}`)
	resp.Body.Close()

	resp = doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", adminJWT,
		`{"role":"maintain","user":{"username":"downgrade-user"}}`)
	resp.Body.Close()

	// Login and create a maintainer-scoped token
	userJWT := loginAndGetJWT(t, pikoURL, "downgrade-user", "pass123")

	var token apitoken.WithPlaintext
	resp = doJSONRequest(t, http.MethodPost, pikoURL+"/api-tokens", userJWT,
		`{"name":"maintainer-token","personal":false,"team_canonical":"main","role":"maintain"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var createResp thttp.CreateApiTokenResponse
	decodeBody(t, resp, &createResp)
	resp.Body.Close()
	require.Empty(t, createResp.Err)
	token = *createResp.Token

	// Token should be able to create pipelines (maintainer action)
	t.Run("maintainer token can create pipeline", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", token.Plaintext,
			`{"name":"downgrade-test","config":"am9iICJ0ZXN0IiB7CiAgdGFzayAiZWNobyIgewogICAgcnVuICJleGVjIiB7CiAgICAgIHBhdGggPSAiZWNobyIKICAgICAgYXJncyA9IFsiaGVsbG8iXQogICAgfQogIH0KfQo="}`)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// Downgrade user to viewer
	resp = doJSONRequest(t, http.MethodPut, pikoURL+"/teams/main/members/downgrade-user", adminJWT,
		`{"role":"read","user":{"username":"downgrade-user"}}`)
	resp.Body.Close()

	// Token should now be denied for maintainer actions (effective = min(viewer, maintainer) = viewer)
	t.Run("after downgrade token cannot create pipeline", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", token.Plaintext,
			`{"name":"should-fail","config":"am9iICJ0ZXN0IiB7fQo="}`)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// But viewer actions should still work
	t.Run("after downgrade token can still list pipelines", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", token.Plaintext, "")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func itoa(n uint32) string {
	return fmt.Sprintf("%d", n)
}
