// Package trigger defines the domain model for triggers in PikoCI.
// A trigger records that a new version of a resource has been detected,
// which may cause downstream jobs to start builds.
package trigger

import "time"

// Trigger represents a recorded event indicating that a new resource version
// was detected. Triggers drive the scheduling of downstream builds.
type Trigger struct {
	ID        uint32                 `json:"id"`
	Name      string                 `json:"name"`
	Version   map[string]interface{} `json:"version"`
	CreatedAt time.Time              `json:"created_at"`
}
