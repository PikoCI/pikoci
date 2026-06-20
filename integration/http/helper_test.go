//go:build integration

package http_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/stretchr/testify/require"
)

// doJSONRequest makes an HTTP request with Content-Type: application/json and optional Bearer token.
func doJSONRequest(t *testing.T, method, url, jwt, body string) *http.Response {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// loginAndGetJWT posts to /login and returns the JWT string.
func loginAndGetJWT(t *testing.T, serverURL, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(thttp.UserLoginRequest{
		Username: username,
		Password: password,
	})
	resp := doJSONRequest(t, http.MethodPost, serverURL+"/login", "", string(body))
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var lr thttp.UserLoginResponse
	err := json.NewDecoder(resp.Body).Decode(&lr)
	require.NoError(t, err)
	require.Empty(t, lr.Err, "login error: %s", lr.Err)
	require.NotEmpty(t, lr.Data.JWT)
	return lr.Data.JWT
}

// requireOK asserts status 200.
func requireOK(t *testing.T, resp *http.Response) {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// decodeBody decodes JSON response body into target.
func decodeBody(t *testing.T, resp *http.Response, target interface{}) {
	t.Helper()
	err := json.NewDecoder(resp.Body).Decode(target)
	require.NoError(t, err)
}
