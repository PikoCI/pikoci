//go:build integration

package http_test

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/secret"
	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
)

// secretPipelineHCL references one stored secret and one stored plain value,
// so a resolve returns both kinds and marks only the plain one as plain.
const secretPipelineHCL = `
variable "gh_token" {
  type = string
  secret "pikoci" {
    key = "CFG_TOKEN"
  }
}

variable "log_level" {
  type = string
  secret "pikoci" {
    key = "CFG_LOG_LEVEL"
  }
}

job "deploy" {
  task "run" {
    run "exec" {
      path = "echo"
      args = ["${var.gh_token}", "${var.log_level}"]
    }
  }
}
`

// findEntry returns the named entry, or nil when the list does not carry it.
func findEntry(entries []*secret.Entry, name string) *secret.Entry {
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func listTeamEntries(t *testing.T, jwt, tc string) []*secret.Entry {
	t.Helper()
	resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/"+tc+"/secrets", jwt, "")
	defer resp.Body.Close()
	requireOK(t, resp)
	var got thttp.ListSecretsResponse
	decodeBody(t, resp, &got)
	require.Empty(t, got.Err)
	return got.Data
}

func TestSecretStore_FullLifecycle(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", adminJWT,
		`{"name":"cfg-pipe","config":"`+base64.StdEncoding.EncodeToString([]byte(secretPipelineHCL))+`"}`)
	resp.Body.Close()
	requireOK(t, resp)

	t.Run("store a secret and a plain value at team scope", func(t *testing.T) {
		for _, body := range []string{
			`{"name":"CFG_TOKEN","value":"team-token","kind":"secret"}`,
			`{"name":"CFG_LOG_LEVEL","value":"debug","kind":"plain"}`,
		} {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/secrets", adminJWT, body)
			defer resp.Body.Close()
			requireOK(t, resp)
		}
	})

	t.Run("list returns plain values but never secret ones", func(t *testing.T) {
		entries := listTeamEntries(t, adminJWT, "main")

		tok := findEntry(entries, "CFG_TOKEN")
		require.NotNil(t, tok)
		assert.Equal(t, secret.KindSecret, tok.Kind)
		assert.Empty(t, tok.Value, "a secret value must never come back from the API")

		lvl := findEntry(entries, "CFG_LOG_LEVEL")
		require.NotNil(t, lvl)
		assert.Equal(t, secret.KindPlain, lvl.Kind)
		assert.Equal(t, "debug", lvl.Value)
	})

	t.Run("kind defaults to secret when omitted", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/secrets", adminJWT,
			`{"name":"CFG_NO_KIND","value":"unspecified"}`)
		resp.Body.Close()
		requireOK(t, resp)

		e := findEntry(listTeamEntries(t, adminJWT, "main"), "CFG_NO_KIND")
		require.NotNil(t, e)
		assert.Equal(t, secret.KindSecret, e.Kind,
			"an omitted kind must not store a value in the clear")
		assert.Empty(t, e.Value)
	})

	t.Run("a pipeline entry shadows the team entry of the same name", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/cfg-pipe/secrets", adminJWT,
			`{"name":"CFG_TOKEN","value":"pipe-token","kind":"secret"}`)
		resp.Body.Close()
		requireOK(t, resp)

		resp = doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/cfg-pipe/secrets", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)
		var got thttp.ListSecretsResponse
		decodeBody(t, resp, &got)
		require.NotNil(t, findEntry(got.Data, "CFG_TOKEN"))
	})

	t.Run("a worker token resolves the values the pipeline references", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/worker-token", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)
		var tok thttp.GenerateTeamWorkerTokenResponse
		decodeBody(t, resp, &tok)
		require.NotEmpty(t, tok.Token)

		resp = doJSONRequest(t, http.MethodGet,
			pikoURL+"/teams/main/pipelines/cfg-pipe/secret-values", tok.Token, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.SecretValuesResponse
		decodeBody(t, resp, &got)
		require.Empty(t, got.Err)

		// The pipeline-scoped entry wins over the team one of the same name.
		assert.Equal(t, "pipe-token", got.Data["CFG_TOKEN"])
		assert.Equal(t, "debug", got.Data["CFG_LOG_LEVEL"])

		// Only the plain value may be unmasked in build logs.
		assert.True(t, got.Plain["CFG_LOG_LEVEL"])
		assert.False(t, got.Plain["CFG_TOKEN"])

		// Entries the pipeline never references stay out of reach.
		assert.NotContains(t, got.Data, "CFG_NO_KIND")
	})

	t.Run("a user JWT cannot reach the resolve route", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet,
			pikoURL+"/teams/main/pipelines/cfg-pipe/secret-values", adminJWT, "")
		defer resp.Body.Close()
		assert.NotEqual(t, http.StatusOK, resp.StatusCode,
			"resolved plaintext is for workers only")
	})

	t.Run("delete removes the entry", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodDelete,
			pikoURL+"/teams/main/secrets/CFG_NO_KIND", adminJWT, "")
		resp.Body.Close()
		requireOK(t, resp)

		assert.Nil(t, findEntry(listTeamEntries(t, adminJWT, "main"), "CFG_NO_KIND"))
	})
}

// The store used to answer every failure with 400, so a caller could not tell
// a missing entry from a malformed name.
func TestSecretStore_ErrorStatuses(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	for _, tt := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{
			name:   "deleting an entry that is not there is 404",
			method: http.MethodDelete,
			path:   "/teams/main/secrets/NOT_STORED",
			want:   http.StatusNotFound,
		},
		{
			name:   "deleting a pipeline entry that is not there is 404",
			method: http.MethodDelete,
			path:   "/teams/main/pipelines/cfg-pipe/secrets/NOT_STORED",
			want:   http.StatusNotFound,
		},
		{
			name:   "a name that is not an identifier is 400",
			method: http.MethodPost,
			path:   "/teams/main/secrets",
			body:   `{"name":"not a valid name","value":"x","kind":"plain"}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "an unknown kind is 400",
			method: http.MethodPost,
			path:   "/teams/main/secrets",
			body:   `{"name":"CFG_BAD_KIND","value":"x","kind":"neither"}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "a malformed body is 400",
			method: http.MethodPost,
			path:   "/teams/main/secrets",
			body:   `{"name":`,
			want:   http.StatusBadRequest,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := doJSONRequest(t, tt.method, pikoURL+tt.path, adminJWT, tt.body)
			defer resp.Body.Close()
			assert.Equal(t, tt.want, resp.StatusCode)

			// Every error keeps the shape the client and the UI read.
			var got thttp.ErrorResponse
			decodeBody(t, resp, &got)
			assert.NotEmpty(t, got.Err)
		})
	}
}

func TestSecretStore_RBAC(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	users := []struct{ username, password, role string }{
		{"cfg-read", "pass1234", "read"},
		{"cfg-maintain", "pass1234", "maintain"},
	}
	for _, u := range users {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users", adminJWT,
			`{"username":"`+u.username+`","password":"`+u.password+`","full_name":"`+u.username+`"}`)
		resp.Body.Close()
		requireOK(t, resp)

		resp = doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", adminJWT,
			`{"role":"`+u.role+`","user":{"username":"`+u.username+`"}}`)
		resp.Body.Close()
		requireOK(t, resp)
	}

	jwts := map[string]string{}
	for _, u := range users {
		jwts[u.role] = loginAndGetJWT(t, pikoURL, u.username, u.password)
	}

	t.Run("read can list", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/secrets", jwts["read"], "")
		defer resp.Body.Close()
		requireOK(t, resp)
	})

	t.Run("read cannot store", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/secrets", jwts["read"],
			`{"name":"CFG_DENIED","value":"x","kind":"plain"}`)
		defer resp.Body.Close()
		assert.NotEqual(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("read cannot delete", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main/secrets/CFG_TOKEN", jwts["read"], "")
		defer resp.Body.Close()
		assert.NotEqual(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("maintain can store and delete", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/secrets", jwts["maintain"],
			`{"name":"CFG_BY_MAINTAIN","value":"x","kind":"plain"}`)
		resp.Body.Close()
		requireOK(t, resp)

		resp = doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main/secrets/CFG_BY_MAINTAIN", jwts["maintain"], "")
		resp.Body.Close()
		requireOK(t, resp)
	})
}
