package team

import "context"

//go:generate go tool mockgen -destination=../mock/team_repository.go -mock_names=Repository=TeamRepository -package mock github.com/pikoci/pikoci/pikoci/team Repository

type Repository interface {
	Create(ctx context.Context, t Team) (uint32, error)
	Update(ctx context.Context, tc string, t Team) error
	Find(ctx context.Context, tc string) (*WithMembers, error)
	Filter(ctx context.Context, un string) ([]*WithMembers, error)
	Delete(ctx context.Context, tc string) error

	CreateMember(ctx context.Context, tc string, tm Member) error
	UpdateMember(ctx context.Context, tc, mc string, tm Member) error
	FindMember(ctx context.Context, tc, mc string) (*Member, error)
	DeleteMember(ctx context.Context, tc, mc string) error
}
