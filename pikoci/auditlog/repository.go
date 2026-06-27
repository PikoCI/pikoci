package auditlog

import (
	"context"
	"time"
)

//go:generate go tool mockgen -destination=../mock/auditlog_repository.go -mock_names=Repository=AuditLogRepository -package mock github.com/pikoci/pikoci/pikoci/auditlog Repository

// Repository defines the persistence operations for audit log entries.
type Repository interface {
	// Create persists a new audit log entry for the given team.
	Create(ctx context.Context, teamCanonical string, e Entry) error
	// Filter returns audit log entries for the given team matching the filter options.
	Filter(ctx context.Context, teamCanonical string, opts FilterOpts) ([]*Entry, error)
}

// FilterOpts specifies filters for querying audit log entries.
// Include and exclude filters are applied as: IN (...) AND NOT IN (...).
// Multiple include values are ORed; multiple exclude values are ORed.
type FilterOpts struct {
	Actors         []string
	ExcludeActors  []string
	Actions        []Action
	ExcludeActions []Action
	Pipelines      []string
	Since          *time.Time
	Until          *time.Time
	Before         *uint32
	After          *uint32
	Limit          uint32
}
