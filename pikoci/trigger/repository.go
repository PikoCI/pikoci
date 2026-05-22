package trigger

import "context"

//go:generate go tool mockgen -destination=../mock/trigger_repository.go -mock_names=Repository=TriggerRepository -package mock github.com/xescugc/pikoci/pikoci/trigger Repository

type Repository interface {
	Create(ctx context.Context, tc, name string, version map[string]interface{}) (*Trigger, error)
	FilterAfter(ctx context.Context, tc, name string, afterID uint32) ([]*Trigger, error)
}
