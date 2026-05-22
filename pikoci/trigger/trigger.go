package trigger

import "time"

type Trigger struct {
	ID        uint32                 `json:"id"`
	Name      string                 `json:"name"`
	Version   map[string]interface{} `json:"version"`
	CreatedAt time.Time              `json:"created_at"`
}
