// Package auditlog defines the domain model for the PikoCI audit trail.
// It records who performed which action on what target within a team.
package auditlog

import "time"

// Action represents the type of audited event.
type Action string

const (
	PipelineCreated  Action = "pipeline.created"
	PipelineUpdated  Action = "pipeline.updated"
	PipelineDeleted  Action = "pipeline.deleted"
	PipelinePaused   Action = "pipeline.paused"
	PipelineUnpaused Action = "pipeline.unpaused"

	JobTriggered Action = "job.triggered"
	JobCancelled Action = "job.cancelled"
	JobRetried   Action = "job.retried"
	JobPaused    Action = "job.paused"
	JobUnpaused  Action = "job.unpaused"

	ResourcePinned  Action = "resource.pinned"
	ResourceUnpinned Action = "resource.unpinned"
	ResourceChecked  Action = "resource.check_triggered"

	BuildApproved      Action = "build.approved"
	BuildRejected      Action = "build.rejected"
	BuildMarkedWarning Action = "build.marked_warning"

	SecretCreated Action = "secret.created"
	SecretDeleted Action = "secret.deleted"

	ConfigCreated Action = "config.created"
	ConfigDeleted Action = "config.deleted"

	MemberAdded       Action = "member.added"
	MemberRemoved     Action = "member.removed"
	MemberRoleChanged Action = "member.role_changed"
)

// Entry represents a single audit log record.
type Entry struct {
	ID         uint32                 `json:"id"`
	Actor      string                 `json:"actor"`
	Action     Action                 `json:"action"`
	TargetType string                 `json:"target_type"`
	TargetName string                 `json:"target_name"`
	Details    map[string]interface{} `json:"details,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}
