package http

import (
	"context"
	"encoding/json"
	"io"
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

func TestOAuthStartRefusesWithoutExternalURL(t *testing.T) {
	svc, server, client := newOAuthTestServer(t, "")
	svc.EXPECT().GetOAuthProvider(gomock.Any(), "example").Return(oauth2TestProvider(), nil)

	resp, err := client.Get(server.URL + "/auth/oauth/example")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "--external-url")
	assert.Empty(t, resp.Header.Get("Location"), "must not redirect to the provider with a relative redirect_uri")
}

func TestGetAdminAuthSettingsIncludesExternalURL(t *testing.T) {
	for _, tc := range []struct {
		name, configured, want string
	}{
		{"set", "https://ci.example.com/", "https://ci.example.com"},
		{"unset", "", ""},
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
			assert.Equal(t, tc.want, got.Data.ExternalURL)
		})
	}
}
