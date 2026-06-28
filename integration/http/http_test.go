//go:build integration

package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikoci/pikoci/pikoci/role"
	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
)

const pipelineHCL = `
resource "cron" "timer" {
  check_interval = "@every 1m"
}

job "test-job" {
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

func TestHTTPEndpoints(t *testing.T) {
	var adminJWT string
	var memberJWT string
	var webhookToken string
	var versionID uint32

	// ---- Auth ----
	t.Run("GetVersion", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodGet, pikoURL+"/version", "", "")
		defer resp.Body.Close()
		requireOK(t, resp)
		var body map[string]string
		decodeBody(t, resp, &body)
		assert.Equal(t, "test", body["version"])
		assert.Equal(t, "abc1234", body["commit"])
	})

	t.Run("Login", func(t *testing.T) {
		adminJWT = loginAndGetJWT(t, pikoURL, "admin", "admin123")
		require.NotEmpty(t, adminJWT)
	})

	t.Run("LoginFail", func(t *testing.T) {
		body, _ := json.Marshal(thttp.UserLoginRequest{Username: "admin", Password: "wrong"})
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/login", "", string(body))
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var lr thttp.UserLoginResponse
		decodeBody(t, resp, &lr)
		assert.NotEmpty(t, lr.Err)
	})

	// ---- Users ----
	t.Run("Users", func(t *testing.T) {
		t.Run("Create", func(t *testing.T) {
			body, _ := json.Marshal(thttp.CreateUserRequest{
				Username: "pepito",
				Password: "pepito123",
				FullName: "Pepito Grillo",
			})
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users", adminJWT, string(body))
			defer resp.Body.Close()
			requireOK(t, resp)
			var cr thttp.CreateUserResponse
			decodeBody(t, resp, &cr)
			assert.Empty(t, cr.Err)
			assert.Equal(t, "pepito", cr.User.Username)
		})

		t.Run("List", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/users", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListUsersResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Users), 2)
		})

		t.Run("Get", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/users/pepito", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var gr thttp.GetUserResponse
			decodeBody(t, resp, &gr)
			assert.Empty(t, gr.Err)
			assert.Equal(t, "pepito", gr.User.Username)
			assert.Equal(t, "Pepito Grillo", gr.User.FullName)
		})

		t.Run("Update", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPut, pikoURL+"/users/pepito", adminJWT, `{"full_name":"Pepito Updated"}`)
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UpdateUserResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
			assert.Equal(t, "Pepito Updated", ur.User.FullName)
		})

		t.Run("UpdateProfile", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPut, pikoURL+"/profile", adminJWT, `{"full_name":"Admin Profile"}`)
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UpdateProfileResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
		})

		t.Run("ChangePassword", func(t *testing.T) {
			memberJWT = loginAndGetJWT(t, pikoURL, "pepito", "pepito123")
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users/change-password", memberJWT, `{"old_password":"pepito123","new_password":"pepito456"}`)
			defer resp.Body.Close()
			requireOK(t, resp)
			var cr thttp.ChangePasswordResponse
			decodeBody(t, resp, &cr)
			assert.Empty(t, cr.Err)

			// Re-login with new password
			memberJWT = loginAndGetJWT(t, pikoURL, "pepito", "pepito456")
		})

		t.Run("Delete", func(t *testing.T) {
			// Create a temp user, then delete
			body, _ := json.Marshal(thttp.CreateUserRequest{
				Username: "temp-user",
				Password: "temp123",
				FullName: "Temp User",
			})
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/users", adminJWT, string(body))
			defer resp.Body.Close()
			requireOK(t, resp)

			resp2 := doJSONRequest(t, http.MethodDelete, pikoURL+"/users/temp-user", adminJWT, "")
			defer resp2.Body.Close()
			requireOK(t, resp2)
			var dr thttp.DeleteUserResponse
			decodeBody(t, resp2, &dr)
			assert.Empty(t, dr.Err)
		})
	})

	// ---- Teams ----
	t.Run("Teams", func(t *testing.T) {
		t.Run("Create", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams", adminJWT, `{"name":"Test Team"}`)
			defer resp.Body.Close()
			requireOK(t, resp)
			var cr thttp.CreateTeamResponse
			decodeBody(t, resp, &cr)
			assert.Empty(t, cr.Err)
			assert.NotNil(t, cr.Team)
			assert.Equal(t, "Test Team", cr.Team.Name)
		})

		t.Run("List", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListTeamsResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Teams), 2) // main + test-team
		})

		t.Run("Get", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/test-team", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var gr thttp.GetTeamResponse
			decodeBody(t, resp, &gr)
			assert.Empty(t, gr.Err)
			assert.Equal(t, "Test Team", gr.Team.Name)
		})

		t.Run("Update", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPut, pikoURL+"/teams/test-team", adminJWT, `{"name":"Test Team Updated"}`)
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UpdateTeamResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
			assert.Equal(t, "Test Team Updated", ur.Team.Name)
		})

		t.Run("Members", func(t *testing.T) {
			t.Run("Create", func(t *testing.T) {
				body := `{"user":{"username":"pepito"},"role":"read"}`
				resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/members", adminJWT, body)
				defer resp.Body.Close()
				requireOK(t, resp)
				var cr thttp.CreateTeamMemberResponse
				decodeBody(t, resp, &cr)
				assert.Empty(t, cr.Err)
			})

			t.Run("Update", func(t *testing.T) {
				resp := doJSONRequest(t, http.MethodPut, pikoURL+"/teams/main/members/pepito", adminJWT, `{"role":"admin"}`)
				defer resp.Body.Close()
				requireOK(t, resp)
				var ur thttp.UpdateTeamMemberResponse
				decodeBody(t, resp, &ur)
				assert.Empty(t, ur.Err)
				assert.Equal(t, role.Admin, ur.Member.Role)
			})

			t.Run("Delete", func(t *testing.T) {
				resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/main/members/pepito", adminJWT, "")
				defer resp.Body.Close()
				requireOK(t, resp)
				var dr thttp.DeleteTeamMemberResponse
				decodeBody(t, resp, &dr)
				assert.Empty(t, dr.Err)
			})
		})

		t.Run("Delete", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodDelete, pikoURL+"/teams/test-team-updated", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var dr thttp.DeleteTeamResponse
			decodeBody(t, resp, &dr)
			assert.Empty(t, dr.Err)
		})
	})

	// ---- Pipelines ----
	t.Run("Pipelines", func(t *testing.T) {
		t.Run("Create", func(t *testing.T) {
			body, _ := json.Marshal(struct {
				Name   string `json:"name"`
				Config []byte `json:"config"`
			}{
				Name:   "test-pipe",
				Config: []byte(pipelineHCL),
			})
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines", adminJWT, string(body))
			defer resp.Body.Close()
			requireOK(t, resp)
			var cr thttp.CreatePipelineResponse
			decodeBody(t, resp, &cr)
			assert.Empty(t, cr.Err)
			assert.NotNil(t, cr.Pipeline)
			assert.Equal(t, "test-pipe", cr.Pipeline.Canonical)
		})

		t.Run("List", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListPipelinesResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Pipelines), 1)
		})

		t.Run("Get", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var gr thttp.GetPipelineResponse
			decodeBody(t, resp, &gr)
			assert.Empty(t, gr.Err)
			assert.Equal(t, "test-pipe", gr.Pipeline.Canonical)
		})

		t.Run("Update", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPut, pikoURL+"/teams/main/pipelines/test-pipe", adminJWT, `{"public":true}`)
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UpdatePipelineResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
		})

		t.Run("Image", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/image.dot", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
		})

		t.Run("Pause", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/pause", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var pr thttp.PausePipelineResponse
			decodeBody(t, resp, &pr)
			assert.Empty(t, pr.Err)
		})

		t.Run("Unpause", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/unpause", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UnpausePipelineResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
		})
	})

	// ---- Jobs ----
	t.Run("Jobs", func(t *testing.T) {
		t.Run("List", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/jobs", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListPipelineJobsResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Jobs), 1)
			found := false
			for _, j := range lr.Jobs {
				if j.Name == "test-job" {
					found = true
				}
			}
			assert.True(t, found, "expected to find test-job")
		})

		t.Run("Get", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var gr thttp.GetPipelineJobResponse
			decodeBody(t, resp, &gr)
			assert.Empty(t, gr.Err)
			assert.Equal(t, "test-job", gr.Job.Name)
		})

		t.Run("Pause", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/pause", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var pr thttp.PauseJobResponse
			decodeBody(t, resp, &pr)
			assert.Empty(t, pr.Err)
		})

		t.Run("Unpause", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/unpause", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UnpauseJobResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
		})

		t.Run("Trigger", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/trigger", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var tr thttp.TriggerPipelineJobResponse
			decodeBody(t, resp, &tr)
			assert.Empty(t, tr.Err)
		})
	})

	// ---- Resources ----
	t.Run("Resources", func(t *testing.T) {
		t.Run("List", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/resources", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListPipelineResourcesResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Resources), 1)
			found := false
			for _, r := range lr.Resources {
				if r.Canonical == "cron.timer" {
					found = true
				}
			}
			assert.True(t, found, "expected to find cron.timer resource")
		})

		t.Run("Get", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var gr thttp.GetPipelineResourceResponse
			decodeBody(t, resp, &gr)
			assert.Empty(t, gr.Err)
			assert.Equal(t, "cron.timer", gr.Resource.Canonical)
			webhookToken = gr.Resource.WebhookToken
		})

		t.Run("Trigger", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer/trigger", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var tr thttp.TriggerPipelineResourceResponse
			decodeBody(t, resp, &tr)
			assert.Empty(t, tr.Err)
		})

		t.Run("CreateVersion", func(t *testing.T) {
			body := `{"version":{"version":{"trigger":"manual"}}}`
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer/versions", adminJWT, body)
			defer resp.Body.Close()
			requireOK(t, resp)
			var cr thttp.CreateResourceVersionResponse
			decodeBody(t, resp, &cr)
			assert.Empty(t, cr.Err)
			require.NotNil(t, cr.Version)
			versionID = cr.Version.ID
			assert.NotZero(t, versionID)
		})

		t.Run("ListVersions", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer/versions", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListResourceVersionsResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Versions), 1)
		})

		t.Run("TriggerVersion", func(t *testing.T) {
			url := fmt.Sprintf("%s/teams/main/pipelines/test-pipe/resources/cron.timer/versions/%d/trigger", pikoURL, versionID)
			resp := doJSONRequest(t, http.MethodPost, url, adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var tr thttp.TriggerResourceVersionResponse
			decodeBody(t, resp, &tr)
			assert.Empty(t, tr.Err)
		})

		t.Run("Pin", func(t *testing.T) {
			body := fmt.Sprintf(`{"version_id":%d}`, versionID)
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer/pin", adminJWT, body)
			defer resp.Body.Close()
			requireOK(t, resp)
			var pr thttp.PinResourceVersionResponse
			decodeBody(t, resp, &pr)
			assert.Empty(t, pr.Err)
		})

		t.Run("Unpin", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer/unpin", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var ur thttp.UnpinResourceVersionResponse
			decodeBody(t, resp, &ur)
			assert.Empty(t, ur.Err)
		})

		t.Run("RegenerateWebhookToken", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/resources/cron.timer/webhook_token", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var rr thttp.RegenerateWebhookTokenResponse
			decodeBody(t, resp, &rr)
			assert.Empty(t, rr.Err)
			assert.NotEmpty(t, rr.Token)
			assert.NotEqual(t, webhookToken, rr.Token)
			webhookToken = rr.Token
		})
	})

	// ---- Webhooks (the original #545 request) ----
	t.Run("Webhooks", func(t *testing.T) {
		t.Run("Trigger", func(t *testing.T) {
			require.NotEmpty(t, webhookToken, "webhook token must be set from Resources.Get or RegenerateWebhookToken")
			req, err := http.NewRequest(http.MethodPost, pikoURL+"/webhooks/"+webhookToken, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			requireOK(t, resp)
			var wr thttp.WebhookTriggerResponse
			decodeBody(t, resp, &wr)
			assert.Empty(t, wr.Err)
		})

		t.Run("TriggerNotFound", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, pikoURL+"/webhooks/bad-token-12345", nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			var wr thttp.WebhookTriggerResponse
			decodeBody(t, resp, &wr)
			assert.NotEmpty(t, wr.Err)
		})
	})

	// ---- Builds ----
	t.Run("Builds", func(t *testing.T) {
		t.Run("List", func(t *testing.T) {
			var builds thttp.ListJobBuildsResponse
			require.Eventually(t, func() bool {
				resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/builds", adminJWT, "")
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return false
				}
				builds = thttp.ListJobBuildsResponse{}
				decodeBody(t, resp, &builds)
				return len(builds.Builds) > 0
			}, 30*time.Second, 500*time.Millisecond, "expected at least one build")
			assert.Empty(t, builds.Err)
		})

		t.Run("Get", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/builds/1", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var gr thttp.GetJobBuildResponse
			decodeBody(t, resp, &gr)
			assert.Empty(t, gr.Err)
			assert.NotNil(t, gr.Build)
		})

		t.Run("Cancel", func(t *testing.T) {
			doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/trigger", adminJWT, "").Body.Close()
			time.Sleep(200 * time.Millisecond)

			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/builds", adminJWT, "")
			defer resp.Body.Close()
			var lr thttp.ListJobBuildsResponse
			decodeBody(t, resp, &lr)
			require.NotEmpty(t, lr.Builds)
			latestNum := lr.Builds[0].BuildNumber

			resp2 := doJSONRequest(t, http.MethodPost, fmt.Sprintf("%s/teams/main/pipelines/test-pipe/jobs/test-job/builds/%s/cancel", pikoURL, latestNum), adminJWT, "")
			defer resp2.Body.Close()
			var cr thttp.CancelJobBuildResponse
			decodeBody(t, resp2, &cr)
		})

		t.Run("Retry", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/pipelines/test-pipe/jobs/test-job/builds/1/retry", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var rr thttp.RetryJobBuildResponse
			decodeBody(t, resp, &rr)
			assert.Empty(t, rr.Err)
		})
	})

	// ---- Workers ----
	t.Run("Workers", func(t *testing.T) {
		t.Run("List", func(t *testing.T) {
			var lr thttp.ListWorkersResponse
			require.Eventually(t, func() bool {
				resp := doJSONRequest(t, http.MethodGet, pikoURL+"/workers", adminJWT, "")
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					return false
				}
				lr = thttp.ListWorkersResponse{}
				decodeBody(t, resp, &lr)
				return len(lr.Workers) > 0
			}, 10*time.Second, 500*time.Millisecond, "expected at least one worker")
			assert.Empty(t, lr.Err)
		})

		t.Run("Health", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/workers/health", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var hr thttp.WorkersHealthResponse
			decodeBody(t, resp, &hr)
			assert.Empty(t, hr.Err)
		})
	})

	// ---- Triggers ----
	t.Run("Triggers", func(t *testing.T) {
		t.Run("Create", func(t *testing.T) {
			body := `{"version":{"key":"value"}}`
			resp := doJSONRequest(t, http.MethodPost, pikoURL+"/teams/main/triggers/test-trigger", adminJWT, body)
			defer resp.Body.Close()
			requireOK(t, resp)
			var cr thttp.CreateTriggerResponse
			decodeBody(t, resp, &cr)
			assert.Empty(t, cr.Err)
			assert.NotNil(t, cr.Trigger)
		})

		t.Run("ListAfter", func(t *testing.T) {
			resp := doJSONRequest(t, http.MethodGet, pikoURL+"/teams/main/triggers/test-trigger", adminJWT, "")
			defer resp.Body.Close()
			requireOK(t, resp)
			var lr thttp.ListTriggersAfterResponse
			decodeBody(t, resp, &lr)
			assert.Empty(t, lr.Err)
			assert.GreaterOrEqual(t, len(lr.Triggers), 1)
		})
	})

	// ---- Admin ----
	t.Run("Admin", func(t *testing.T) {
		t.Run("Export", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, pikoURL+"/admin/export", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+adminJWT)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			requireOK(t, resp)
			assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
		})

		t.Run("ExportUnauth", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, pikoURL+"/admin/export", nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	})

	// ---- Auth edge cases ----
	t.Run("RefreshToken", func(t *testing.T) {
		resp := doJSONRequest(t, http.MethodPost, pikoURL+"/refresh-token", adminJWT, "")
		defer resp.Body.Close()
		requireOK(t, resp)
		var rr thttp.RefreshTokenResponse
		decodeBody(t, resp, &rr)
		assert.Empty(t, rr.Err)
		assert.NotEmpty(t, rr.Data.JWT)
	})
}
