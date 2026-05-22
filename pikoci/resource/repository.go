package resource

import "context"

//go:generate go tool mockgen -destination=../mock/resource_repository.go -mock_names=Repository=ResourceRepository -package mock github.com/pikoci/pikoci/pikoci/resource Repository

type Repository interface {
	Create(ctx context.Context, tc, pn string, r Resource) (uint32, error)
	Update(ctx context.Context, tc, pn, rCan string, r Resource) error
	Find(ctx context.Context, tc, pn, rCan string) (*Resource, error)
	FindByWebhookToken(ctx context.Context, token string) (*Resource, string, string, error)
	Filter(ctx context.Context, tc, pn string) ([]*Resource, error)
	FilterDueResources(ctx context.Context) ([]*ResourceWithPipeline, error)
	Delete(ctx context.Context, tc, pn, rCan string) error

	CreateVersion(ctx context.Context, tc, pn, rCan string, v Version) (uint32, error)
	FilterVersions(ctx context.Context, tc, pn, rCan string) ([]*Version, error)
}

type ResourceWithPipeline struct {
	Resource
	TeamCanonical string
	PipelineName  string
}
