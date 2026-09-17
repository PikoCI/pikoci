package http

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAssetPrefix(t *testing.T) {
	assert.Equal(t, "/assets/abc1234/", assetPrefix("abc1234"))

	// No commit to key on: still a usable prefix, and never the literal
	// "unknown" that would be the same for every dev build.
	for _, c := range []string{"", "unknown"} {
		p := assetPrefix(c)
		assert.True(t, strings.HasPrefix(p, "/assets/"), p)
		assert.True(t, strings.HasSuffix(p, "/"), p)
		assert.NotEqual(t, "/assets/unknown/", p)
		assert.NotEqual(t, "/assets//", p)
	}
}

func TestVersionedAssets(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)

	handler := Handler(s, []byte("test-secret"), slog.Default(), nil, "", "test", "abc1234", "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	get := func(path string) (*http.Response, string) {
		resp, err := http.Get(server.URL + path)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		resp.Body.Close()
		return resp, string(body)
	}

	// The page names the build's prefix and is never served from cache
	// without revalidation.
	resp, body := get("/")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Contains(t, body, `src="/assets/abc1234/js/app/app.js"`)
	assert.Contains(t, body, `"preact": "/assets/abc1234/js/vendor/preact.module.js"`)
	assert.Contains(t, body, `href="/assets/abc1234/css/pikoci.css"`)
	assert.NotContains(t, body, `"/js/`, "every asset reference must go through the prefix")
	assert.NotContains(t, body, `"/css/`, "every asset reference must go through the prefix")

	// Under the prefix the same file is immutable.
	resp, body = get("/assets/abc1234/js/app/graph-zoom.js")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "public, max-age=31536000, immutable", resp.Header.Get("Cache-Control"))
	assert.Contains(t, body, "PikoGraphZoom")

	// A relative import from inside the prefix stays inside it.
	resp, _ = get("/assets/abc1234/js/vendor/preact/hooks.module.js")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The bare paths still work, but must be revalidated on every use.
	resp, body = get("/js/app/graph-zoom.js")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Contains(t, body, "PikoGraphZoom")

	// Anything else under the prefix is a plain 404, not the SPA page.
	resp, _ = get("/assets/abc1234/js/app/no-such-file.js")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestLocalEditorVersionedAssets(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)

	handler := LocalEditorHandler(s, "/nonexistent/pipeline.hcl", slog.Default(), "abc1234")
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Contains(t, string(body), `src="/assets/abc1234/js/app/local-app.js"`)
	assert.NotContains(t, string(body), `"/js/`)

	resp, err = http.Get(server.URL + "/assets/abc1234/js/app/local-app.js")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "public, max-age=31536000, immutable", resp.Header.Get("Cache-Control"))
}
