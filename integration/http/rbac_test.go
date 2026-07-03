//go:build integration

package http_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
)

// TestRBAC_PerRoleAccess creates users with each role and verifies correct
// access/denial for representative endpoints per role level.
func TestRBAC_PerRoleAccess(t *testing.T) {
	// Login as admin to set up users and team members
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	// Create users for each role
	roles := []struct {
		username string
		password string
		role     string
	}{
		{"rbac-viewer", "pass123", "read"},
		{"rbac-operator", "pass123", "write"},
		{"rbac-maintainer", "pass123", "maintain"},
		{"rbac-admin", "pass123", "admin"},
	}

	for _, r := range roles {
		body := `{"username":"` + r.username + `","password":"` + r.password + `","full_name":"` + r.username + `"}`
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users", adminJWT, body)
		resp.Body.Close()
		requireOK(t, resp)
	}

	// Create a pipeline for testing (on the "main" team which admin owns)
	resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", adminJWT,
		`{"name":"rbac-pipe","config":"am9iICJ0ZXN0IiB7CiAgdGFzayAiZWNobyIgewogICAgcnVuICJleGVjIiB7CiAgICAgIHBhdGggPSAiZWNobyIKICAgICAgYXJncyA9IFsiaGVsbG8iXQogICAgfQogIH0KfQo="}`)
	resp.Body.Close()

	// Add each user as a member of "main" with their respective role
	for _, r := range roles {
		body := `{"role":"` + r.role + `","user":{"username":"` + r.username + `"}}`
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", adminJWT, body)
		resp.Body.Close()
		requireOK(t, resp)
	}

	// Get JWTs for each user
	jwts := make(map[string]string)
	for _, r := range roles {
		jwts[r.role] = loginAndGetJWT(t, pikoURL, r.username, r.password)
	}

	// --- Viewer tests ---
	t.Run("Viewer", func(t *testing.T) {
		jwt := jwts["read"]

		t.Run("can GET pipeline", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		t.Run("can GET team", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		t.Run("denied trigger job", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/jobs/test/trigger", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("denied create pipeline", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", jwt, `{"name":"x","config":"eA=="}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("denied add team member", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", jwt, `{"role":"read","user":{"username":"admin"}}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("denied delete team", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	})

	// --- Operator tests ---
	t.Run("Operator", func(t *testing.T) {
		jwt := jwts["write"]

		t.Run("can pause pipeline", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/pause", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		t.Run("can unpause pipeline", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/unpause", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		t.Run("denied create pipeline", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", jwt, `{"name":"x","config":"eA=="}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("denied add team member", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", jwt, `{"role":"read","user":{"username":"admin"}}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	})

	// --- Maintainer tests ---
	t.Run("Maintainer", func(t *testing.T) {
		jwt := jwts["maintain"]

		t.Run("can pause pipeline (inherits operator)", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/pause", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			// Unpause for subsequent tests
			resp2 := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/unpause", jwt, "")
			resp2.Body.Close()
		})

		t.Run("denied add team member", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", jwt, `{"role":"read","user":{"username":"admin"}}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("denied delete team", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main", jwt, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	})

	// --- Admin tests ---
	t.Run("Admin", func(t *testing.T) {
		jwt := jwts["admin"]

		t.Run("can update team", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPut, pikoURL+"/teams/main", jwt, `{"name":"main"}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		t.Run("can delete team", func(t *testing.T) {
			// Create a disposable team via global admin
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams", adminJWT, `{"name":"disposable"}`)
			resp.Body.Close()
			requireOK(t, resp)

			// Add rbac-admin as admin of disposable team
			body := `{"role":"admin","user":{"username":"rbac-admin"}}`
			resp = doJSONRequest(t, http.MethodPost, pikoURL+"/teams/disposable/members", adminJWT, body)
			resp.Body.Close()
			requireOK(t, resp)

			// Refresh JWT to get updated memberships
			freshJWT := loginAndGetJWT(t, pikoURL, "rbac-admin", "pass123")

			resp = doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/disposable", freshJWT, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	})

	// --- Role upgrade test ---
	t.Run("RoleUpgrade", func(t *testing.T) {
		// Upgrade viewer to operator
		resp := doJSONRequest(t, http.MethodPut, pikoURL+"/teams/main/members/rbac-viewer", adminJWT, `{"role":"write"}`)
		defer resp.Body.Close()
		requireOK(t, resp)
		var ur thttp.UpdateTeamMemberResponse
		decodeBody(t, resp, &ur)
		require.Empty(t, ur.Err)

		// Get fresh JWT for the upgraded user
		upgradedJWT := loginAndGetJWT(t, pikoURL, "rbac-viewer", "pass123")

		// Now viewer-turned-operator can pause
		resp2 := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/pause", upgradedJWT, "")
		defer resp2.Body.Close()
		assert.Equal(t, http.StatusOK, resp2.StatusCode)

		// Unpause
		resp3 := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/rbac-pipe/unpause", upgradedJWT, "")
		resp3.Body.Close()
	})

	// --- Team worker token RBAC ---
	t.Run("TeamWorkerToken", func(t *testing.T) {
		t.Run("admin can generate token", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/worker-token", jwts["admin"], "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			var got thttp.GenerateTeamWorkerTokenResponse
			decodeBody(t, resp, &got)
			assert.Empty(t, got.Err)
			assert.NotEmpty(t, got.Token)
		})

		t.Run("admin can get token", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/worker-token", jwts["admin"], "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			var got thttp.GetTeamWorkerTokenResponse
			decodeBody(t, resp, &got)
			assert.Empty(t, got.Err)
			assert.NotEmpty(t, got.Token)
		})

		t.Run("maintain denied generate token", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/worker-token", jwts["maintain"], "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("write denied get token", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/worker-token", jwts["write"], "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("read denied get token", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/worker-token", jwts["read"], "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	})

	// --- Last admin protection ---
	t.Run("LastAdminProtection", func(t *testing.T) {
		// Create a team where admin is the sole admin
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams", adminJWT, `{"name":"admin-test"}`)
		resp.Body.Close()
		requireOK(t, resp)

		t.Run("cannot delete last admin", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/admin-test/members/admin", adminJWT, "")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var dr thttp.DeleteTeamMemberResponse
			decodeBody(t, resp, &dr)
			assert.Contains(t, dr.Err, "no admins")
		})

		t.Run("cannot demote last admin", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPut, pikoURL+"/teams/admin-test/members/admin", adminJWT, `{"role":"read"}`)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var ur thttp.UpdateTeamMemberResponse
			decodeBody(t, resp, &ur)
			assert.Contains(t, ur.Err, "no admins")
		})

		t.Run("can delete non-admin when admin remains", func(t *testing.T) {
			// Add a viewer
			addResp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/admin-test/members", adminJWT,
				`{"role":"read","user":{"username":"rbac-viewer"}}`)
			addResp.Body.Close()
			requireOK(t, addResp)

			// Delete the viewer — should succeed
			resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/admin-test/members/rbac-viewer", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var dr thttp.DeleteTeamMemberResponse
			decodeBody(t, resp, &dr)
			assert.Empty(t, dr.Err)
		})

		// Cleanup
		doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/admin-test", adminJWT, "").Body.Close()
	})
}
