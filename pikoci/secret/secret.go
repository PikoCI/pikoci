// Package secret defines the domain model for secrets in PikoCI.
// Secrets store sensitive configuration values (such as credentials or tokens)
// that are resolved at build time through secret types.
package secret

// Secret represents a named secret associated with a pipeline. It has a type
// that determines how the secret value is retrieved, and optional parameters
// passed to the secret type's get command.
type Secret struct {
	ID        uint32            `json:"id"`
	Type      string            `json:"type" hcl:"type,label"`
	Name      string            `json:"name" hcl:"name,label"`
	Canonical string            `json:"canonical"`
	Params    map[string]string `json:"params,omitempty"`
}
