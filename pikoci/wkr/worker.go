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
	Concurrency int       `json:"concurrency"`
	Queues      string    `json:"queues"`
	StartedAt   time.Time `json:"started_at"`
	LastPingAt  time.Time `json:"last_ping_at"`
	Status      Status    `json:"status"`
}

// ComputeStatus sets the worker's Status based on how recently it pinged.
func (w *Worker) ComputeStatus(now time.Time) {
	if now.Sub(w.LastPingAt) > StaleThreshold {
		w.Status = StatusStale
	} else {
		w.Status = StatusHealthy
	}
}
