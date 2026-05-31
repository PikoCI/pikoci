package utils

// RunnerCommand represents a command to be executed by a named runner. It is
// used in resource type and service definitions to specify check, pull, put,
// start, and stop operations.
type RunnerCommand struct {
	Runner string            `json:"runner" hcl:"runner,label"`
	Args   []string          `json:"args" hcl:"args,optional"`
	Params map[string]string `json:"params" hcl:",remain"`
}

// RunnerOverride allows a type definition to override the runner used for all
// its commands. When present, exec commands are transformed to run under the
// specified runner (e.g. docker) instead of directly on the host.
type RunnerOverride struct {
	Runner string            `json:"runner" hcl:"runner,label"`
	Args   []string          `json:"args,omitempty" hcl:"args,optional"`
	Params map[string]string `json:"params,omitempty" hcl:",remain"`
}

// RunCommand represents a simple command execution with an optional working
// directory path and arguments. It is used in job step definitions.
type RunCommand struct {
	Path string   `json:"path" hcl:"path,optional"`
	Args []string `json:"args" hcl:"args,optional"`
}
