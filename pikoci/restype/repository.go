package restype

import "context"

//go:generate go tool mockgen -destination=../mock/resource_type_repository.go -mock_names=Repository=ResourceTypeRepository -package mock github.com/pikoci/pikoci/pikoci/restype Repository

type Repository interface {
	Create(ctx context.Context, tc, pn string, rt ResourceType) (uint32, error)
	Update(ctx context.Context, tc, pn, tn string, rt ResourceType) error
	Find(ctx context.Context, tc, pn, tn string) (*ResourceType, error)
	Filter(ctx context.Context, tc, pn string) ([]*ResourceType, error)
	Delete(ctx context.Context, tc, pn, tn string) error
}
