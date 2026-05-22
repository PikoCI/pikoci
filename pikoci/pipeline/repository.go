package pipeline

import (
	"context"
)

//go:generate go tool mockgen -destination=../mock/pipeline_repository.go -mock_names=Repository=PipelineRepository -package mock github.com/pikoci/pikoci/pikoci/pipeline Repository

type Repository interface {
	Create(ctx context.Context, tc string, pp Pipeline) (uint32, error)
	Update(ctx context.Context, tc, ppn string, pp Pipeline) error
	Find(ctx context.Context, tc, pn string) (*Pipeline, error)
	FindPublic(ctx context.Context, tc, pn string) (*Pipeline, error)
	Filter(ctx context.Context, tc string) ([]*Pipeline, error)
	FilterAll(ctx context.Context) ([]*WithTeam, error)
	SetPublic(ctx context.Context, tc, pn string, public bool) error
	Delete(ctx context.Context, tc, pn string) error
}
