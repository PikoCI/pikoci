package trigger

import "context"

//go:generate go tool mockgen -destination=../mock/trigger_repository.go -mock_names=Repository=TriggerRepository -package mock github.com/xescugc/pikoci/pikoci/trigger Repository

// Repository defines the persistence operations for triggers.
type Repository interface {
	// Create persists a new trigger with the given version metadata, returning the created trigger.
	Create(ctx context.Context, tc, name string, version map[string]interface{}) (*Trigger, error)
	// FilterAfter returns all triggers for the given team and resource name created after the specified trigger ID.
	FilterAfter(ctx context.Context, tc, name string, afterID uint32) ([]*Trigger, error)
}
