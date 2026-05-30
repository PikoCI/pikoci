// Package notiftype defines the domain model for notification types in PikoCI.
// A notification type specifies how to send fire-and-forget notifications
// using a runner command.
package notiftype

import "github.com/pikoci/pikoci/pikoci/utils"

// NotificationType defines the behavior for a category of notifications. It specifies
// the runner command used to send notifications. Notification types can be loaded
// from a remote source.
type NotificationType struct {
	ID     uint32               `json:"id"`
	Name   string               `json:"name" hcl:"name,label"`
	Source string               `json:"source,omitempty" hcl:"source,optional"`
	Notify *utils.RunnerCommand `json:"notify,omitempty" hcl:"notify,block"`
	Params []string             `json:"params" hcl:"params,optional"`
}
