package wkr

//go:generate go tool mockgen -destination=../mock/worker_repository.go -mock_names=Repository=WorkerRepository -package mock github.com/pikoci/pikoci/pikoci/wkr Repository

import (
	"context"
	"time"
)

// Repository defines the persistence operations for workers.
type Repository interface {
	Upsert(ctx context.Context, w Worker) error
	Filter(ctx context.Context) ([]*Worker, error)
	DeleteBefore(ctx context.Context, before time.Time) error
	DeleteByName(ctx context.Context, name string) error
}
