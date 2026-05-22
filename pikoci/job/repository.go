package job

import "context"

//go:generate go tool mockgen -destination=../mock/job_repository.go -mock_names=Repository=JobRepository -package mock github.com/pikoci/pikoci/pikoci/job Repository

type Repository interface {
	Create(ctx context.Context, tc, pn string, j Job) (uint32, error)
	Update(ctx context.Context, tc, pn, jn string, j Job) error
	Find(ctx context.Context, tc, pn, jn string) (*Job, error)
	Filter(ctx context.Context, tc, pn string) ([]*Job, error)
	Delete(ctx context.Context, tc, pn, jn string) error
}
