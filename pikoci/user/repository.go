package user

import "context"

//go:generate go tool mockgen -destination=../mock/user_repository.go -mock_names=Repository=UserRepository -package mock github.com/pikoci/pikoci/pikoci/user Repository

type Repository interface {
	Create(ctx context.Context, u User) (uint32, error)
	Update(ctx context.Context, un string, u User) error
	Find(ctx context.Context, un string) (*User, error)
	FindWithMemberships(ctx context.Context, un string) (*WithMemberships, error)
	Filter(ctx context.Context) ([]*User, error)
	Delete(ctx context.Context, un string) error
}
