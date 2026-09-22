package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/pikoci/pikoci/pikoci/oauthprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newOAuthTestServer builds a handler with the given --external-url and a
// live OAuth state store, and a client that does not follow redirects so the
// provider redirect can be inspected.
func newOAuthTestServer(t *testing.T, externalURL string) (*mock.Service, *httptest.Server, *http.Client) {
	t.Helper()
	ctrl := gomock.NewController(t)
	svc := mock.NewService(ctrl)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stateStore := pikoci.NewOAuthStateStore(ctx)
	handler := Handler(svc, []byte("test-secret"), slog.Default(), nil, "", "test", "abc1234", externalURL, stateStore)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return svc, server, client
}

func oauth2TestProvider() *oauthprovider.Provider {
	return &oauthprovider.Provider{
		ID:        1,
		Name:      "Example",
		Canonical: "example",
		Type:      "oauth2",
		AuthURL:   "https://idp.example.com/authorize",
		TokenURL:  "https://idp.example.com/token",
		Scopes:    "openid",
		ClientID:  "pikoci",
		Enabled:   true,
	}
}

func TestOAuthStartUsesExternalURLForCallback(t *testing.T) {
	svc, server, client := newOAuthTestServer(t, "https://ci.example.com/")
	svc.EXPECT().GetOAuthProvider(gomock.Any(), "example").Return(oauth2TestProvider(), nil)

	resp, err := client.Get(server.URL + "/auth/oauth/example")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "idp.example.com", loc.Host)
	assert.Equal(t, "https://ci.example.com/auth/oauth/example/callback", loc.Query().Get("redirect_uri"))
	assert.NotEmpty(t, loc.Query().Get("state"))
}

func TestOAuthStartDerivesRedirectURIFromRequestWithoutExternalURL(t *testing.T) {
	svc, server, client := newOAuthTestServer(t, "")
	svc.EXPECT().GetOAuthProvider(gomock.Any(), "example").Return(oauth2TestProvider(), nil).Times(2)

	t.Run("host header", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/auth/oauth/example")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusFound, resp.StatusCode)
		loc, err := url.Parse(resp.Header.Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, server.URL+"/auth/oauth/example/callback", loc.Query().Get("redirect_uri"))
	})

	t.Run("forwarded headers", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/auth/oauth/example", nil)
		require.NoError(t, err)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "ci.example.com, internal-lb")
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusFound, resp.StatusCode)
		loc, err := url.Parse(resp.Header.Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "https://ci.example.com/auth/oauth/example/callback", loc.Query().Get("redirect_uri"))
	})
}

func TestResolveExternalURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/auth/oauth/x", nil)
	assert.Equal(t, "https://ci.example.com", resolveExternalURL(" https://ci.example.com/ ", req), "configured URL wins and is trimmed")
	assert.Equal(t, "http://localhost:8080", resolveExternalURL("", req))

	req.TLS = &tls.ConnectionState{}
	assert.Equal(t, "https://localhost:8080", resolveExternalURL("", req))

	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "ci.example.com")
	assert.Equal(t, "http://ci.example.com", resolveExternalURL("", req), "forwarded headers override the connection")
}

func TestGetAdminAuthSettingsIncludesExternalURL(t *testing.T) {
	for _, tc := range []struct {
		name, configured string
		wantConfigured   bool
	}{
		{"set", "https://ci.example.com/", true},
		{"unset", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			// Rebuild the server with the external URL under test.
			e.server.Close()
			e.server = httptest.NewServer(Handler(e.svc, []byte("test-secret"), slog.Default(), nil, "", "test", "abc1234", tc.configured, nil))
			t.Cleanup(e.server.Close)

			e.expectAdminAuth()
			e.svc.EXPECT().GetAuthSettings(gomock.Any()).Return(&oauthprovider.AuthSettings{ID: 1, LocalAuthEnabled: true}, nil)

			resp := doRequest(t, http.MethodGet, e.server.URL+"/admin/auth-settings", e.adminJWT(t), "")
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var got GetAdminAuthSettingsResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
			assert.Empty(t, got.Err)
			assert.True(t, got.Data.LocalAuthEnabled)
			assert.Equal(t, tc.wantConfigured, got.Data.ExternalURLConfigured)
			if tc.wantConfigured {
				assert.Equal(t, "https://ci.example.com", got.Data.ExternalURL)
			} else {
				assert.Equal(t, e.server.URL, got.Data.ExternalURL, "falls back to the request's own URL")
			}
		})
	}
}
