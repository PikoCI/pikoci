package notification

import "context"

//go:generate go tool mockgen -destination=../mock/notification_repository.go -mock_names=Repository=NotificationRepository -package mock github.com/pikoci/pikoci/pikoci/notification Repository

// Repository defines the persistence operations for notifications.
type Repository interface {
	// Create persists a new notification in the given team and pipeline, returning its ID.
	Create(ctx context.Context, tc, pn string, n Notification) (uint32, error)
	// Update updates an existing notification identified by team, pipeline, and canonical.
	Update(ctx context.Context, tc, pn, nCan string, n Notification) error
	// Find retrieves a notification by team, pipeline, and canonical.
	Find(ctx context.Context, tc, pn, nCan string) (*Notification, error)
	// Filter returns all notifications belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*Notification, error)
	// Delete removes a notification identified by team, pipeline, and canonical.
	Delete(ctx context.Context, tc, pn, nCan string) error
}
