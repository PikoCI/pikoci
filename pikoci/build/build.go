package build

import (
	"errors"
	"time"
)

var ErrNotPending = errors.New("build is not in pending status")

//go:generate go tool enumer -type=Status -transform=snake -output=status_string.go -json

type Status int

const (
	Succeeded Status = iota
	Failed
	Started
	Cancelled
	Pending
)

// Build represents a run of a Job
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
}

type Step struct {
	Type      string        `json:"type"`
	Name      string        `json:"name"`
	VersionID uint32        `json:"version_id"`
	Logs      string        `json:"logs"`
	Duration  time.Duration `json:"duration"`
	Status    Status        `json:"status"`
}
