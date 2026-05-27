package sectype

import "github.com/pikoci/pikoci/pikoci/utils"

type SecretType struct {
	ID     uint32              `json:"id"`
	Name   string              `json:"name" hcl:"name,label"`
	Source string              `json:"source,omitempty" hcl:"source,optional"`
	Params []string            `json:"params" hcl:"params,optional"`
	Config map[string]string   `json:"config,omitempty"`
	Get    utils.RunnerCommand `json:"get" hcl:"get,block"`
}
