package user

import "context"

//go:generate go tool mockgen -destination=../mock/user_repository.go -mock_names=Repository=UserRepository -package mock github.com/pikoci/pikoci/pikoci/user Repository

// Repository defines the persistence operations for users.
type Repository interface {
	// Create persists a new user, returning the user ID.
	Create(ctx context.Context, u User) (uint32, error)
	// Update updates an existing user identified by username.
	Update(ctx context.Context, un string, u User) error
	// Find retrieves a user by username.
	Find(ctx context.Context, un string) (*User, error)
	// FindWithMemberships retrieves a user by username along with all team memberships.
	FindWithMemberships(ctx context.Context, un string) (*WithMemberships, error)
	// Filter returns all users in the system.
	Filter(ctx context.Context) ([]*User, error)
	// Delete removes a user identified by username.
	Delete(ctx context.Context, un string) error
}
