package client

import (
	"context"
	"fmt"
	"net/http"

	thttp "github.com/xescugc/pikoci/pikoci/transport/http"
	"github.com/xescugc/pikoci/pikoci/trigger"
)

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
