package pikoci

import (
	"context"
	"fmt"
	"time"

	"github.com/pikoci/pikoci/pikoci/wkr"
)

func (q *PikoCI) WorkerHeartbeat(ctx context.Context, w wkr.Worker) error {
	if q.Workers == nil {
		return nil
	}
	if w.Name == "" {
		return fmt.Errorf("worker name is required")
	}
	w.LastPingAt = time.Now()
	err := q.Workers.Upsert(ctx, w)
	if err != nil {
		return fmt.Errorf("failed to upsert worker: %w", err)
	}
	return nil
}

func (q *PikoCI) ListWorkers(ctx context.Context) ([]*wkr.Worker, error) {
	if q.Workers == nil {
		return nil, nil
	}
	workers, err := q.Workers.Filter(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list workers: %w", err)
	}
	return workers, nil
}

func (q *PikoCI) WorkersHealth(ctx context.Context) (bool, error) {
	if q.Workers == nil {
		return false, nil
	}
	workers, err := q.Workers.Filter(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check workers health: %w", err)
	}
	for _, w := range workers {
		if w.Status == wkr.StatusHealthy {
			return true, nil
		}
	}
	return false, nil
}

func (q *PikoCI) DeleteWorker(ctx context.Context, name string) error {
	if q.Workers == nil {
		return nil
	}
	if name == "" {
		return fmt.Errorf("worker name is required")
	}
	err := q.Workers.DeleteByName(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to delete worker: %w", err)
	}
	return nil
}

