package runner

import "context"

//go:generate go tool mockgen -destination=../mock/runner_repository.go -mock_names=Repository=RunnerRepository -package mock github.com/pikoci/pikoci/pikoci/runner Repository

type Repository interface {
	Create(ctx context.Context, tc, pn string, ru Runner) (uint32, error)
	Find(ctx context.Context, tc, pn, run string) (*Runner, error)
	Filter(ctx context.Context, tc, pn string) ([]*Runner, error)
	Update(ctx context.Context, tc, pn, run string, ru Runner) error
	Delete(ctx context.Context, tc, pn, run string) error
}
