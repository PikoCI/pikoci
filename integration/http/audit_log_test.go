//go:build integration

package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
)

const auditTestPipelineHCL = `
resource "cron" "timer" {
  check_interval = "@every 10m"
}

job "build" {
  get "cron" "timer" {
    trigger = true
  }
  task "echo" {
    run "exec" {
      path = "echo"
      args = ["hello"]
    }
  }
}
`

func TestAuditLog(t *testing.T) {
	adminJWT := loginAndGetJWT(t, pikoURL, "admin", "admin123")

	// Step 1: Create a pipeline (generates pipeline.created audit entry)
	t.Run("CreatePipeline", func(t *testing.T) {
		body, _ := json.Marshal(struct {
			Name   string `json:"name"`
			Config []byte `json:"config"`
		}{
			Name:   "audit-test-pipe",
			Config: []byte(auditTestPipelineHCL),
		})
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", adminJWT, string(body))
		defer resp.Body.Close()
		requireOK(t, resp)
	})

	// Step 2: Pause the pipeline (generates pipeline.paused)
	t.Run("PausePipeline", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/audit-test-pipe/pause", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)
	})

	// Step 3: Unpause the pipeline (generates pipeline.unpaused)
	t.Run("UnpausePipeline", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/audit-test-pipe/unpause", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)
	})

	// Step 4: List audit log — should have at least 3 entries from our actions
	t.Run("ListAll", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?user=admin&pipeline=audit-test-pipe", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		require.GreaterOrEqual(t, len(got.Entries), 3, "expected at least 3 audit entries")

		actions := make([]string, len(got.Entries))
		for i, e := range got.Entries {
			actions[i] = string(e.Action)
		}
		assert.Contains(t, actions, "pipeline.created")
		assert.Contains(t, actions, "pipeline.paused")
		assert.Contains(t, actions, "pipeline.unpaused")
	})

	// Step 5: Filter by action
	t.Run("FilterByAction", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?action=pipeline.created", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		for _, e := range got.Entries {
			assert.Equal(t, "pipeline.created", string(e.Action))
		}
	})

	// Step 6: Exclude action
	t.Run("ExcludeAction", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?exclude_action=pipeline.created", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		for _, e := range got.Entries {
			assert.NotEqual(t, "pipeline.created", string(e.Action))
		}
	})

	// Step 7: Filter by pipeline
	t.Run("FilterByPipeline", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?pipeline=audit-test-pipe", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		require.GreaterOrEqual(t, len(got.Entries), 3)
	})

	// Step 8: Filter by user
	t.Run("FilterByUser", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?user=admin", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		for _, e := range got.Entries {
			assert.Equal(t, "admin", e.Actor)
		}
	})

	// Step 9: Exclude user
	t.Run("ExcludeUser", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?exclude_user=admin", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		for _, e := range got.Entries {
			assert.NotEqual(t, "admin", e.Actor)
		}
	})

	// Step 10: Pagination with limit
	t.Run("Pagination", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?limit=1", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		assert.Len(t, got.Entries, 1)
		assert.NotNil(t, got.Meta)
		assert.True(t, got.Meta.HasMore)

		// Load next page
		resp2 := doJSONRequest(t, http.MethodGet,
			pikoURL+"/teams/main/audit?limit=1&before="+fmt.Sprintf("%d", got.Meta.OldestID),
			adminJWT, "")
		defer resp2.Body.Close()
		requireOK(t, resp2)

		var got2 thttp.ListAuditLogResponse
		decodeBody(t, resp2, &got2)
		assert.Empty(t, got2.Err)
		assert.Len(t, got2.Entries, 1)
		assert.NotEqual(t, got.Entries[0].ID, got2.Entries[0].ID, "page 2 should have different entry")
	})

	// Step 11: Empty result for non-existent filter
	t.Run("EmptyResult", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?user=nonexistent", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		assert.Empty(t, got.Entries)
		assert.Nil(t, got.Meta)
	})

	// Step 12: Delete the pipeline (generates pipeline.deleted)
	t.Run("DeletePipeline", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main/pipelines/audit-test-pipe", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)
	})

	// Step 13: Verify delete was audited
	t.Run("DeleteAudited", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/audit?action=pipeline.deleted", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)

		var got thttp.ListAuditLogResponse
		decodeBody(t, resp, &got)
		assert.Empty(t, got.Err)
		require.GreaterOrEqual(t, len(got.Entries), 1)
		assert.Equal(t, "audit-test-pipe", got.Entries[0].TargetName)
	})
}

