// Package resource defines the domain model for versioned resources in PikoCI.
// Resources represent external artifacts (such as Git repositories or container
// images) that are checked for new versions and used as inputs/outputs in jobs.
package resource

import "time"

// Resource represents a versioned external artifact tracked by a pipeline.
// Each resource has a type, a name, optional parameters, and a check interval
// that controls how frequently new versions are polled.
type Resource struct {
	ID   uint32 `json:"id"`
	Type string `json:"type" hcl:"type,label"`
	Name string `json:"name" hcl:"name,label"`

	Params        *Params `json:"params,omitempty" hcl:"params,block"`
	CheckInterval string  `json:"check_interval" hcl:"check_interval,optional"`

	Cache *bool `json:"cache,omitempty" hcl:"cache,optional"`

	PinnedVersionID *uint32 `json:"pinned_version_id,omitempty"`

	Canonical    string    `json:"canonical"`
	Logs         string    `json:"logs"`
	LastCheck    time.Time `json:"last_check"`
	NextCheck    time.Time `json:"next_check"`
	WebhookToken string    `json:"webhook_token"`
}

// GetParams returns the resource's parameters as a string map, or nil if no
// parameters are set.
func (r Resource) GetParams() map[string]string {
	if r.Params == nil {
		return nil
	}
	return r.Params.Params
}

// Params holds a map of key-value parameters for a resource, parsed from HCL.
type Params struct {
	Params map[string]string `json:"params" hcl:",remain"`
}

// Version represents a specific version of a resource, identified by an ID
// and a map of version metadata returned by the resource type's check command.
type Version struct {
	ID      uint32                 `json:"id"`
	Version map[string]interface{} `json:"version"`
}
