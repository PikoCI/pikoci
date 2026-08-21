package client

import (
	"context"
	"fmt"
	"net/http"

	thttp "github.com/pikoci/pikoci/pikoci/transport/http"
	"github.com/pikoci/pikoci/pikoci/trigger"
)

// FireTriggerNotifications asks the server to fire on_trigger notifications
// for all jobs reachable from the given resource canonical.
func (cl *Client) FireTriggerNotifications(ctx context.Context, tc, pc, triggeringRCan string, versionMeta map[string]interface{}) {
	// Fire-and-forget: remote workers delegate notification execution to the
	// server, which runs the exec command in a temp dir. Errors are logged
	// server-side; the worker does not wait for a meaningful response.
	var resp thttp.FireTriggerNotificationsResponse
	_ = cl.Request(ctx, http.MethodPost,
		fmt.Sprintf("%s/teams/%s/pipelines/%s/trigger-notifications", cl.url, tc, pc),
		thttp.FireTriggerNotificationsRequest{
			TriggeringResourceCanonical: triggeringRCan,
			VersionMeta:                 versionMeta,
		},
		&resp,
	)
}

// CreateTrigger creates a new trigger event with the given name and version data.
func (cl *Client) CreateTrigger(ctx context.Context, tc, name string, version map[string]interface{}) (*trigger.Trigger, error) {
	var resp thttp.CreateTriggerResponse

	err := cl.Request(ctx, http.MethodPost, fmt.Sprintf("%s/teams/%s/triggers/%s", cl.url, tc, name), thttp.CreateTriggerRequest{
		Version: version,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Trigger, nil
}

// ListTriggersAfter retrieves all triggers with an ID greater than afterID.
func (cl *Client) ListTriggersAfter(ctx context.Context, tc, name string, afterID uint32) ([]*trigger.Trigger, error) {
	var resp thttp.ListTriggersAfterResponse

	err := cl.Request(ctx, http.MethodGet, fmt.Sprintf("%s/teams/%s/triggers/%s?after=%d", cl.url, tc, name, afterID), nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.Err != "" {
		return nil, fmt.Errorf("error from request: %s", resp.Err)
	}

	return resp.Triggers, nil
}
