// Package queue defines the work-item types exchanged between the server and
// workers. Topic and Subscription interfaces have been removed; workers now
// receive work via HTTP long-poll instead of pub/sub.
package queue

// WorkItem represents a unit of work for a worker to process.
type WorkItem struct {
	Type string `json:"type"` // "job" or "check"
	Body Body   `json:"body"`
}

// Body is the JSON-serializable payload carried inside every work item.
// Different fields are populated depending on the work type to identify the
// target team, pipeline, job, resource, or build.
type Body struct {
	TeamCanonical     string `json:"team_canonical,omitempty"`
	PipelineCanonical string `json:"pipeline_canonical,omitempty"`
	JobName           string `json:"job_name,omitempty"`
	ResourceCanonical string `json:"resource_canonical,omitempty"`
	VersionID         uint32 `json:"version_id,omitempty"`
	BuildID           uint32 `json:"build_id,omitempty"`
	RetryBuildNumber  string `json:"retry_build_number,omitempty"` // parent build number for retry numbering
	RetryBuildID      uint32 `json:"retry_build_id,omitempty"`     // build ID to copy resource versions from
}
