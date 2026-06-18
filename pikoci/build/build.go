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
)

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

	VersionID         uint32 `json:"version_id,omitempty"`
	ResourceCanonical string `json:"resource_canonical,omitempty"`

	RetrySourceBuildID uint32 `json:"retry_source_build_id,omitempty"`

	// SuppressUpdates prevents updateBuild from persisting this build.
	// Used by in_parallel goroutines that operate on local build copies.
	SuppressUpdates bool `json:"-"`

	// OnUpdate is called by updateBuild when SuppressUpdates is true.
	// Used by in_parallel goroutines to sync their steps to the parent build.
	OnUpdate func() `json:"-"`
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
