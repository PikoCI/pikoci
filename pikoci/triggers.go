package pikoci

import (
	"context"
	"fmt"

	"github.com/xescugc/pikoci/pikoci/trigger"
	"github.com/xescugc/pikoci/pikoci/utils"
)

// CreateTrigger creates a new trigger event with the given name and version data
// within a team.
func (q *PikoCI) CreateTrigger(ctx context.Context, tc, name string, version map[string]interface{}) (*trigger.Trigger, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}
	if name == "" {
		return nil, fmt.Errorf("trigger name cannot be empty")
	}

	t, err := q.Triggers.Create(ctx, tc, name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to Create Trigger: %w", err)
	}

	return t, nil
}

// ListTriggersAfter returns all trigger events with IDs greater than afterID for
// the given trigger name within a team. This supports long-polling for new events.
func (q *PikoCI) ListTriggersAfter(ctx context.Context, tc, name string, afterID uint32) ([]*trigger.Trigger, error) {
	if !utils.ValidateCanonical(tc) {
		return nil, fmt.Errorf("invalid Team Canonical format %q", tc)
	}
	if name == "" {
		return nil, fmt.Errorf("trigger name cannot be empty")
	}

	triggers, err := q.Triggers.FilterAfter(ctx, tc, name, afterID)
	if err != nil {
		return nil, fmt.Errorf("failed to List Triggers: %w", err)
	}

	return triggers, nil
}
