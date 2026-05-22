package secret

import "context"

//go:generate go tool mockgen -destination=../mock/secret_repository.go -mock_names=Repository=SecretRepository -package mock github.com/pikoci/pikoci/pikoci/secret Repository

type Repository interface {
	Create(ctx context.Context, tc, pn string, s Secret) (uint32, error)
	Update(ctx context.Context, tc, pn, sCan string, s Secret) error
	Find(ctx context.Context, tc, pn, sCan string) (*Secret, error)
	Filter(ctx context.Context, tc, pn string) ([]*Secret, error)
	Delete(ctx context.Context, tc, pn, sCan string) error
}
