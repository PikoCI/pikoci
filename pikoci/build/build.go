// Package build defines the domain model for build execution in PikoCI.
// A build represents a single run of a job within a pipeline, tracking its
// status, steps, duration, and associated resource versions.
package build

import (
	"errors"
	"time"
)

// ErrNotPending is returned when an operation requires a build to be in pending
// status but the build has a different status.
var ErrNotPending = errors.New("build is not in pending status")

//go:generate go tool enumer -type=Status -transform=snake -output=status_string.go -json

// Status represents the current state of a build execution.
type Status int

const (
	// Succeeded indicates the build completed successfully.
	Succeeded Status = iota
	// Failed indicates the build finished with an error.
	Failed
	// Started indicates the build is currently running.
	Started
	// Cancelled indicates the build was cancelled before completion.
	Cancelled
	// Pending indicates the build is queued and waiting to start.
	Pending
	// WaitingForApproval indicates the build is waiting for human approval before starting.
	WaitingForApproval
	// Warning indicates the build failed but the job has allow_failure enabled,
	// so downstream jobs are not blocked.
	Warning
)

// Approval represents a single approve/reject vote on a build.
type Approval struct {
	ID        uint32    `json:"id"`
	BuildID   uint32    `json:"build_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"` // "approved" or "rejected"
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Build represents a single execution of a job within a pipeline.
type Build struct {
	ID          uint32 `json:"id"`
	BuildNumber string `json:"build_number"`
	Steps       []Step `json:"steps"`
	Status      Status `json:"status"`
	Error       string `json:"error"`
	// Job are the general logs printed at the end
	Job []Step `json:"job"`

	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"duration"`

	VersionID         uint32                 `json:"version_id,omitempty"`
	ResourceCanonical string                 `json:"resource_canonical,omitempty"`
	VersionMetadata   map[string]interface{} `json:"version_metadata,omitempty"`

	// PinnedVersions maps get-step resource canonical names to their version
	// metadata. Populated at build creation time for approval gate builds so
	// the exact versions are tracked and displayed.
	PinnedVersions map[string]map[string]interface{} `json:"pinned_versions,omitempty"`

	RetrySourceBuildID uint32 `json:"retry_source_build_id,omitempty"`

	// Approvals contains the approval/rejection votes for this build.
	// Only populated for builds with WaitingForApproval status.
	Approvals []Approval `json:"approvals,omitempty"`

	// SuppressUpdates prevents updateBuild from persisting this build.
	// Used by in_parallel goroutines that operate on local build copies.
	SuppressUpdates bool `json:"-"`

	// OnUpdate is called by updateBuild when SuppressUpdates is true.
	// Used by in_parallel goroutines to sync their steps to the parent build.
	OnUpdate func() `json:"-"`
}

// BuildReport is a structured JSON report containing all runtime data for a build.
// It wraps the build data with team/pipeline/job context and a generation timestamp.
type BuildReport struct {
	ReportVersion   string                            `json:"report_version"`
	GeneratedAt     time.Time                         `json:"generated_at"`
	Team            string                            `json:"team"`
	Pipeline        string                            `json:"pipeline"`
	Job             string                            `json:"job"`
	Build           BuildReportData                   `json:"build"`
	Approvals       []Approval                        `json:"approvals"`
	Steps           []Step                            `json:"steps"`
	JobLogs         []Step                            `json:"job_logs"`
}

// BuildReportData contains the build-specific fields for the report.
type BuildReportData struct {
	Number            string                            `json:"number"`
	Status            string                            `json:"status"`
	Error             string                            `json:"error"`
	StartedAt         time.Time                         `json:"started_at"`
	Duration          time.Duration                     `json:"duration"`
	VersionID         uint32                            `json:"version_id"`
	ResourceCanonical string                            `json:"resource_canonical"`
	VersionMetadata   map[string]interface{}            `json:"version_metadata"`
	PinnedVersions    map[string]map[string]interface{} `json:"pinned_versions"`
	RetryOf           string                            `json:"retry_of"`
}

// Step represents an individual step within a build, such as a get, put, or task
// operation. Each step tracks its own logs, duration, and completion status.
type Step struct {
	Type      string        `json:"type"`
	Name      string        `json:"name"`
	VersionID uint32        `json:"version_id"`
	Logs      string        `json:"logs"`
	Duration  time.Duration `json:"duration"`
	Status    Status        `json:"status"`
	SubSteps  []Step        `json:"sub_steps,omitempty"`
}
