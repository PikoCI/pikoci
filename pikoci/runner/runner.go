// Package runner defines the domain model for runners in PikoCI.
// A runner provides the execution environment for task steps in a pipeline,
// defining how commands are run (e.g., via shell, Docker, or other backends).
package runner

import "github.com/xescugc/pikoci/pikoci/utils"

// Runner represents an execution environment used to run task steps. It
// defines the command used to execute tasks and can optionally be loaded
// from a remote source.
type Runner struct {
	ID     uint32           `json:"id"`
	Name   string           `json:"name" hcl:"name,label"`
	Source string           `json:"source,omitempty" hcl:"source,optional"`
	Run    utils.RunCommand `json:"run" hcl:"run,block"`
}
