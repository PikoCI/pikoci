// Package restype defines the domain model for resource types in PikoCI.
// A resource type specifies how to check for new versions, pull artifacts,
// and push artifacts for a given kind of resource.
package restype

import "github.com/pikoci/pikoci/pikoci/utils"

// ResourceType defines the behavior for a category of resources. It specifies
// the runner commands used to check for new versions, pull resource content,
// and push new versions. Resource types can be loaded from a remote source.
type ResourceType struct {
	ID     uint32   `json:"id"`
	Name   string   `json:"name" hcl:"name,label"`
	Source string   `json:"source,omitempty" hcl:"source,optional"`
	Params []string `json:"params" hcl:"params,optional"`

	Check *utils.RunnerCommand `json:"check,omitempty" hcl:"check,block"`
	Pull  *utils.RunnerCommand `json:"pull,omitempty" hcl:"pull,block"`
	Push  *utils.RunnerCommand `json:"push,omitempty" hcl:"push,block"`
}
