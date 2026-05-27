package sectype

import "context"

//go:generate go tool mockgen -destination=../mock/secret_type_repository.go -mock_names=Repository=SecretTypeRepository -package mock github.com/pikoci/pikoci/pikoci/sectype Repository

type Repository interface {
	Create(ctx context.Context, tc, pn string, st SecretType) (uint32, error)
	Update(ctx context.Context, tc, pn, stn string, st SecretType) error
	Find(ctx context.Context, tc, pn, stn string) (*SecretType, error)
	Filter(ctx context.Context, tc, pn string) ([]*SecretType, error)
	Delete(ctx context.Context, tc, pn, stn string) error
}
