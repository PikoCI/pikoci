package wkr

import "time"

type Status string

const (
	StatusHealthy Status = "healthy"
	StatusStale   Status = "stale"
)

const StaleThreshold = 90 * time.Second

type Worker struct {
	ID          uint32    `json:"id"`
	Name        string    `json:"name"`
	Hostname    string    `json:"hostname"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	GoVersion   string    `json:"go_version"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	Concurrency int       `json:"concurrency"`
	Tags            []string  `json:"tags"`
	ExclusiveTags   bool      `json:"exclusive_tags"`
	TeamCanonical   string    `json:"team_canonical"`
	StartedAt       time.Time `json:"started_at"`
	LastPingAt      time.Time `json:"last_ping_at"`
	Status          Status    `json:"status"`
}

// ComputeStatus sets the worker's Status based on how recently it pinged.
func (w *Worker) ComputeStatus(now time.Time) {
	if now.Sub(w.LastPingAt) > StaleThreshold {
		w.Status = StatusStale
	} else {
		w.Status = StatusHealthy
	}
}
