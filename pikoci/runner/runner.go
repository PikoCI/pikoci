package runner

import "github.com/pikoci/pikoci/pikoci/utils"

type Runner struct {
	ID     uint32           `json:"id"`
	Name   string           `json:"name" hcl:"name,label"`
	Source string           `json:"source,omitempty" hcl:"source,optional"`
	Run    utils.RunCommand `json:"run" hcl:"run,block"`
}
