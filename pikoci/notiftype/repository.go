package notiftype

import "context"

//go:generate go tool mockgen -destination=../mock/notification_type_repository.go -mock_names=Repository=NotificationTypeRepository -package mock github.com/pikoci/pikoci/pikoci/notiftype Repository

// Repository defines the persistence operations for notification types.
type Repository interface {
	// Create persists a new notification type in the given team and pipeline, returning its ID.
	Create(ctx context.Context, tc, pn string, nt NotificationType) (uint32, error)
	// Update updates an existing notification type identified by team, pipeline, and type name.
	Update(ctx context.Context, tc, pn, tn string, nt NotificationType) error
	// Find retrieves a notification type by team, pipeline, and type name.
	Find(ctx context.Context, tc, pn, tn string) (*NotificationType, error)
	// Filter returns all notification types belonging to the given team and pipeline.
	Filter(ctx context.Context, tc, pn string) ([]*NotificationType, error)
	// Delete removes a notification type identified by team, pipeline, and type name.
	Delete(ctx context.Context, tc, pn, tn string) error
}
