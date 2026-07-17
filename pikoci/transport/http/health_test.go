package http

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pikoci/pikoci/pikoci/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	_ "modernc.org/sqlite"
)

func TestHealth_OK(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	handler := Handler(s, []byte("test-secret"), slog.Default(), db, "sqlite", "1.0.0", "abc123", "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "connected", body["db"])
	assert.Equal(t, "1.0.0", body["version"])
}

func TestHealth_DBDown(t *testing.T) {
	ctrl := gomock.NewController(t)
	s := mock.NewService(ctrl)

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.Close() // close to simulate unhealthy DB

	handler := Handler(s, []byte("test-secret"), slog.Default(), db, "sqlite", "1.0.0", "abc123", "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "error", body["status"])
	assert.Equal(t, "disconnected", body["db"])
	assert.NotEmpty(t, body["error"])
}
