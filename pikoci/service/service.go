// Package service defines the domain model for sidecar services in PikoCI.
// Services are long-running processes (such as databases or caches) that run
// alongside job steps and are started before the job plan executes.
package service

import "github.com/pikoci/pikoci/pikoci/utils"

// Service represents a sidecar service that runs during job execution. It
// defines commands for starting and stopping the service, an optional ready
// check to wait for the service to become available, and can be loaded from
// a remote source.
type Service struct {
	ID         uint32              `json:"id"`
	Name       string              `json:"name" hcl:"name,label"`
	Source     string              `json:"source,omitempty" hcl:"source,optional"`
	Params     []string            `json:"params" hcl:"params,optional"`
	Start      utils.RunnerCommand `json:"start"`
	ReadyCheck *ReadyCheck         `json:"ready_check,omitempty"`
	Stop       utils.RunnerCommand `json:"stop"`
}

// ReadyCheck defines a health check that determines when a service is ready
// to accept connections. It embeds a RunnerCommand and adds configurable
// polling interval and timeout durations.
type ReadyCheck struct {
	utils.RunnerCommand
	Interval string `json:"interval" hcl:"interval,optional"`
	Timeout  string `json:"timeout" hcl:"timeout,optional"`
}
