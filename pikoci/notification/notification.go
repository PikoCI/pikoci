// Package notification defines the domain model for notifications in PikoCI.
// Notifications are fire-and-forget messages sent during or after job execution,
// configured with parameters, optional automatic dispatch rules, and job scoping.
package notification

// Notification represents a fire-and-forget notification attached to a pipeline.
// Notifications are sent via notification types and can be triggered automatically
// based on build events or explicitly in job plans and hooks.
type Notification struct {
	ID        uint32   `json:"id"`
	Type      string   `json:"type" hcl:"type,label"`
	Name      string   `json:"name" hcl:"name,label"`
	Canonical string   `json:"canonical"`
	Params    *Params  `json:"params,omitempty" hcl:"params,block"`
	Message   string   `json:"message,omitempty" hcl:"message,optional"`
	On        []string `json:"on,omitempty" hcl:"on,optional"`
	Jobs      []string `json:"jobs,omitempty" hcl:"jobs,optional"`
	Exclude   []string `json:"exclude,omitempty" hcl:"exclude,optional"`
}

// GetParams returns the notification's parameters as a string map, or nil if no
// parameters are set.
func (n Notification) GetParams() map[string]string {
	if n.Params == nil {
		return nil
	}
	return n.Params.Params
}

// Params holds a map of key-value parameters for a notification, parsed from HCL.
type Params struct {
	Params map[string]string `json:"params" hcl:",remain"`
}
