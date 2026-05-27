package source_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/source"
)

func TestResolveResourceType_PikoCI(t *testing.T) {
	rt, err := source.ResolveResourceType(context.Background(), "pikoci://git")
	require.NoError(t, err)
	assert.Equal(t, "git", rt.Name)
	assert.Equal(t, "exec", rt.Check.Runner)
}

func TestResolveResourceType_HTTP(t *testing.T) {
	hcl := `
resource_type "custom" {
  params = ["url"]
  check "exec" {
    path = "/bin/sh"
    args = ["-c", "echo ok"]
  }
  pull "exec" { }
  push "exec" { }
}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(hcl))
	}))
	defer srv.Close()

	rt, err := source.ResolveResourceType(context.Background(), srv.URL+"/custom.hcl")
	require.NoError(t, err)
	assert.Equal(t, "custom", rt.Name)
	assert.Equal(t, []string{"url"}, rt.Params)
}

func TestResolveResourceType_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := source.ResolveResourceType(context.Background(), srv.URL+"/notfound.hcl")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

func TestResolveResourceType_InvalidHCL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid hcl {{{{"))
	}))
	defer srv.Close()

	_, err := source.ResolveResourceType(context.Background(), srv.URL+"/bad.hcl")
	require.Error(t, err)
}

func TestResolveResourceType_UnsupportedScheme(t *testing.T) {
	_, err := source.ResolveResourceType(context.Background(), "ftp://example.com/foo.hcl")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported source URL scheme")
}

func TestResolveRunner_PikoCI(t *testing.T) {
	ru, err := source.ResolveRunner(context.Background(), "pikoci://exec")
	require.NoError(t, err)
	assert.Equal(t, "exec", ru.Name)
	assert.Equal(t, "$path", ru.Run.Path)
}

func TestResolveService_HTTP(t *testing.T) {
	hcl := `
service_type "mydb" {
  params = ["version", "port"]
  start "exec" {
    path = "/bin/sh"
    args = ["-c", "echo start"]
  }
  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-c", "echo ready"]
    interval = "3s"
    timeout  = "30s"
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-c", "echo stop"]
  }
}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(hcl))
	}))
	defer srv.Close()

	svc, err := source.ResolveService(context.Background(), srv.URL+"/mydb.hcl")
	require.NoError(t, err)
	assert.Equal(t, "mydb", svc.Name)
	assert.Equal(t, []string{"version", "port"}, svc.Params)
	assert.Equal(t, "exec", svc.Start.Runner)
	assert.Equal(t, "exec", svc.Stop.Runner)
	require.NotNil(t, svc.ReadyCheck)
	assert.Equal(t, "exec", svc.ReadyCheck.Runner)
	assert.Equal(t, "3s", svc.ReadyCheck.Interval)
	assert.Equal(t, "30s", svc.ReadyCheck.Timeout)
}

func TestResolveService_NoReadyCheck(t *testing.T) {
	hcl := `
service_type "simple" {
  start "exec" {
    path = "/bin/sh"
    args = ["-c", "echo start"]
  }
  stop "exec" {
    path = "/bin/sh"
    args = ["-c", "echo stop"]
  }
}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(hcl))
	}))
	defer srv.Close()

	svc, err := source.ResolveService(context.Background(), srv.URL+"/simple.hcl")
	require.NoError(t, err)
	assert.Equal(t, "simple", svc.Name)
	assert.Equal(t, "exec", svc.Start.Runner)
	assert.Equal(t, "exec", svc.Stop.Runner)
	assert.Nil(t, svc.ReadyCheck)
}

func TestResolveRunner_HTTP(t *testing.T) {
	hcl := `
runner_type "myrunner" {
  run {
    path = "/usr/bin/env"
    args = ["bash", "-c", "$script"]
  }
}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(hcl))
	}))
	defer srv.Close()

	ru, err := source.ResolveRunner(context.Background(), srv.URL+"/myrunner.hcl")
	require.NoError(t, err)
	assert.Equal(t, "myrunner", ru.Name)
	assert.Equal(t, "/usr/bin/env", ru.Run.Path)
}
