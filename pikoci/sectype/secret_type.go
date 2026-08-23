// Package sectype defines the domain model for secret types in PikoCI.
// A secret type specifies how secrets of a given kind are retrieved,
// using a runner command to fetch secret values at build time.
package sectype

import "github.com/pikoci/pikoci/pikoci/utils"

// StoreName is the reserved secret type name that resolves values from
// PikoCI's own encrypted secret store instead of running a command. It is
// handled natively by the worker, so it carries no Get command.
const StoreName = "pikoci"

// Store returns the synthetic secret type for the built-in encrypted store.
// It exists so pipelines can reference `secret "pikoci"` without declaring a
// secret_type block, and so reference validation recognises the name.
func Store() SecretType {
	return SecretType{Name: StoreName}
}

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

	Runner *utils.RunnerOverride `json:"runner,omitempty" hcl:"runner,block"`
}
