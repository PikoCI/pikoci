// Package sectype defines the domain model for secret types in PikoCI.
// A secret type specifies how secrets of a given kind are retrieved,
// using a runner command to fetch secret values at build time.
package sectype

import "github.com/xescugc/pikoci/pikoci/utils"

// SecretType defines the behavior for a category of secrets. It specifies
// the runner command used to retrieve secret values. Secret types can be
// loaded from a remote source and accept configuration parameters.
type SecretType struct {
	ID     uint32              `json:"id"`
	Name   string              `json:"name" hcl:"name,label"`
	Source string              `json:"source,omitempty" hcl:"source,optional"`
	Params []string            `json:"params" hcl:"params,optional"`
	Config map[string]string   `json:"config,omitempty"`
	Get    utils.RunnerCommand `json:"get" hcl:"get,block"`
}
