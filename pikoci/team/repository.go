package team

import "context"

//go:generate go tool mockgen -destination=../mock/team_repository.go -mock_names=Repository=TeamRepository -package mock github.com/pikoci/pikoci/pikoci/team Repository

// Repository defines the persistence operations for teams and their members.
type Repository interface {
	// Create persists a new team, returning the team ID.
	Create(ctx context.Context, t Team) (uint32, error)
	// Update updates an existing team identified by its canonical name.
	Update(ctx context.Context, tc string, t Team) error
	// Find retrieves a team with its members by canonical name.
	Find(ctx context.Context, tc string) (*WithMembers, error)
	// Filter returns all teams that the given user belongs to, including members.
	Filter(ctx context.Context, un string) ([]*WithMembers, error)
	// Delete removes a team identified by its canonical name.
	Delete(ctx context.Context, tc string) error

	// CreateMember adds a member to a team.
	CreateMember(ctx context.Context, tc string, tm Member) error
	// UpdateMember updates a member's role in a team, identified by team and member canonical.
	UpdateMember(ctx context.Context, tc, mc string, tm Member) error
	// FindMember retrieves a specific member of a team by team and member canonical.
	FindMember(ctx context.Context, tc, mc string) (*Member, error)
	// DeleteMember removes a member from a team.
	DeleteMember(ctx context.Context, tc, mc string) error
}
