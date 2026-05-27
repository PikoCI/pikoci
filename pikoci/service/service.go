package service

import "github.com/pikoci/pikoci/pikoci/utils"

type Service struct {
	ID         uint32              `json:"id"`
	Name       string              `json:"name" hcl:"name,label"`
	Source     string              `json:"source,omitempty" hcl:"source,optional"`
	Params     []string            `json:"params" hcl:"params,optional"`
	Start      utils.RunnerCommand `json:"start"`
	ReadyCheck *ReadyCheck         `json:"ready_check,omitempty"`
	Stop       utils.RunnerCommand `json:"stop"`
}

type ReadyCheck struct {
	utils.RunnerCommand
	Interval string `json:"interval" hcl:"interval,optional"`
	Timeout  string `json:"timeout" hcl:"timeout,optional"`
}
