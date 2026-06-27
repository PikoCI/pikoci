package pikoci

import (
	"context"

	"github.com/pikoci/pikoci/pikoci/auditlog"
)

// audit records an audit log entry. It is fire-and-forget: failures are logged
// but not propagated to the caller.
func (q *PikoCI) audit(ctx context.Context, tc string, action auditlog.Action, targetType, targetName string, details map[string]interface{}) {
	if q.AuditLogs == nil {
		return
	}
	actor := "system"
	if un, ok := ctx.Value(ActorContextKey).(string); ok && un != "" {
		actor = un
	}
	entry := auditlog.Entry{
		Actor:      actor,
		Action:     action,
		TargetType: targetType,
		TargetName: targetName,
		Details:    details,
	}
	if err := q.AuditLogs.Create(ctx, tc, entry); err != nil {
		q.logger.Error("failed to write audit log", "error", err, "action", string(action), "team", tc)
	}
}

// ListAuditLog returns audit log entries for the given team matching the filter
// options. It uses the limit+1 pattern for has_more detection.
func (q *PikoCI) ListAuditLog(ctx context.Context, tc string, opts auditlog.FilterOpts) ([]*auditlog.Entry, bool, error) {
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	// Fetch one extra to detect has_more
	opts.Limit++
	entries, err := q.AuditLogs.Filter(ctx, tc, opts)
	if err != nil {
		return nil, false, err
	}
	opts.Limit-- // restore original
	hasMore := len(entries) > int(opts.Limit)
	if hasMore {
		entries = entries[:opts.Limit]
	}
	return entries, hasMore, nil
}
