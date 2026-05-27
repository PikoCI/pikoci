// Package pipeline defines the domain model for CI/CD pipelines in PikoCI.
// A pipeline aggregates jobs, resources, resource types, runners, secret types,
// services, and variables. It supports parsing from HCL configuration files.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/xescugc/pikoci/pikoci/builtin"
	"github.com/xescugc/pikoci/pikoci/job"
	"github.com/xescugc/pikoci/pikoci/resource"
	"github.com/xescugc/pikoci/pikoci/restype"
	"github.com/xescugc/pikoci/pikoci/runner"
	"github.com/xescugc/pikoci/pikoci/sectype"
	"github.com/xescugc/pikoci/pikoci/service"
	"github.com/xescugc/pikoci/pikoci/source"
	"github.com/xescugc/pikoci/pikoci/team"
	"github.com/xescugc/pikoci/pikoci/utils"
	"github.com/zclconf/go-cty/cty"
)

// TypeEvalContext returns an HCL eval context with the type pseudo-variables
// (string, number, bool) needed to parse variable declarations.
func TypeEvalContext() *hcl.EvalContext {
	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"string": cty.StringVal("string"),
			"number": cty.StringVal("number"),
			"bool":   cty.StringVal("bool"),
		},
	}
}

// WithTeam embeds a Pipeline along with its owning Team.
type WithTeam struct {
	Pipeline
	Team team.Team
}

// Pipeline represents a complete CI/CD pipeline configuration. It contains all
// jobs, resources, resource types, runners, secret types, services, and
// variable declarations that define the pipeline's behavior.
type Pipeline struct {
	ID            uint32                    `json:"id"`
	Name          string                    `json:"name"`
	Canonical     string                    `json:"canonical"`
	Public        bool                      `json:"public"`
	Jobs          []job.Job                 `json:"jobs" hcl:"job,block"`
	Resources     []resource.Resource       `json:"resources" hcl:"resource,block"`
	ResourceTypes []restype.ResourceType    `json:"resource_types" hcl:"resource_type,block"`
	Runners       []runner.Runner           `json:"runners" hcl:"runner_type,block"`
	SecretTypes   []sectype.SecretType      `json:"secret_types" hcl:"secret_type,block"`
	Services      []service.Service         `json:"services" hcl:"service_type,block"`
	SecretVars    map[string]VariableSecret `json:"secret_vars,omitempty"`
	Remain        hcl.Body                  `json:"-" hcl:",remain"`
	Raw           []byte                    `json:"raw"`
	LastBuildAt   *time.Time                `json:"last_build_at,omitempty"`
}

// Variables holds the list of variable declarations parsed from pipeline HCL.
type Variables struct {
	Variables []Variable `json:"variables" hcl:"variable,block"`
	Remain    hcl.Body   `hcl:",remain"`
}

// Variable represents a single variable declaration in a pipeline. Variables
// can have a type, a default value, and an optional secret reference.
type Variable struct {
	Name    string          `json:"name" hcl:"name,label"`
	Type    string          `json:"type" hcl:"type"`
	Default interface{}     `json:"default" hcl:"default,optional"`
	Secret  *VariableSecret `json:"secret,omitempty" hcl:"secret,block"`
}

// VariableSecret references a secret value for a pipeline variable. The Type
// identifies the secret type, Path is an optional path within the secret store,
// and Key specifies which key to retrieve.
type VariableSecret struct {
	Type string `json:"type" hcl:"type,label"`
	Path string `json:"path,omitempty" hcl:"path,optional"`
	Key  string `json:"key" hcl:"key"`
}

// ResourceType looks up a resource type by name in the pipeline's configured
// resource types, falling back to built-in resource types. It returns the
// resource type and true if found, or an empty value and false otherwise.
func (pp *Pipeline) ResourceType(rtn string) (restype.ResourceType, bool) {
	for _, rt := range pp.ResourceTypes {
		if rt.Name == rtn {
			return rt, true
		}
	}

	if brt, ok := builtin.ResourceTypes()[rtn]; ok {
		return brt, true
	}

	return restype.ResourceType{}, false
}

// Resource looks up a resource by its canonical identifier. It returns the
// resource and true if found, or an empty value and false otherwise.
func (pp *Pipeline) Resource(rCan string) (resource.Resource, bool) {
	for _, r := range pp.Resources {
		if r.Canonical == rCan {
			return r, true
		}
	}

	return resource.Resource{}, false
}

// Runner looks up a runner by name in the pipeline's configured runners,
// falling back to built-in runners. It returns the runner and true if found,
// or an empty value and false otherwise.
func (pp *Pipeline) Runner(run string) (runner.Runner, bool) {
	for _, ru := range pp.Runners {
		if ru.Name == run {
			return ru, true
		}
	}

	if bru, ok := builtin.Runners()[run]; ok {
		return bru, true
	}

	return runner.Runner{}, false
}

// SecretType looks up a secret type by name in the pipeline's configured secret
// types. It returns the secret type and true if found, or an empty value and
// false otherwise.
func (pp *Pipeline) SecretType(stn string) (sectype.SecretType, bool) {
	for _, st := range pp.SecretTypes {
		if st.Name == stn {
			return st, true
		}
	}

	return sectype.SecretType{}, false
}

// Service looks up a service by name in the pipeline's configured services. It
// returns the service and true if found, or an empty value and false otherwise.
func (pp *Pipeline) Service(name string) (service.Service, bool) {
	for _, s := range pp.Services {
		if s.Name == name {
			return s, true
		}
	}

	return service.Service{}, false
}

// hclReadyCheckRaw is used for parsing ready_check blocks from raw HCL.
type hclReadyCheckRaw struct {
	Runner   string            `hcl:"runner,label"`
	Args     []string          `hcl:"args,optional"`
	Interval string            `hcl:"interval,optional"`
	Timeout  string            `hcl:"timeout,optional"`
	Params   map[string]string `hcl:",remain"`
}

// hclServiceRaw is used for parsing top-level service blocks from raw HCL.
type hclServiceRaw struct {
	Name       string                `hcl:"name,label"`
	Source     string                `hcl:"source,optional"`
	Params     []string              `hcl:"params,optional"`
	Start      []utils.RunnerCommand `hcl:"start,block"`
	ReadyCheck []hclReadyCheckRaw    `hcl:"ready_check,block"`
	Stop       []utils.RunnerCommand `hcl:"stop,block"`
}

// hclPipelineServices is a minimal struct for parsing only service blocks from raw HCL.
type hclPipelineServices struct {
	Services []hclServiceRaw `hcl:"service_type,block"`
	Remain   hcl.Body        `hcl:",remain"`
}

// ParseServicesFromRaw parses service definitions from raw pipeline HCL.
// This extracts both top-level service blocks and inline service definitions
// inside job blocks. Used to populate the Services field on pipelines loaded
// from the database, where services are not stored in a separate table.
func ParseServicesFromRaw(ctx context.Context, raw []byte) ([]service.Service, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	ectx, err := buildVarEvalContext(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve variables for service parsing: %w", err)
	}

	// Parse top-level service blocks
	var hp hclPipelineServices
	err = hclsimple.Decode("pipeline.hcl", raw, ectx, &hp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse services from raw HCL: %w", err)
	}

	serviceByName := make(map[string]bool)
	var services []service.Service
	for _, hs := range hp.Services {
		if hs.Source != "" {
			resolved, err := source.ResolveService(ctx, hs.Source)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve source for service_type %q: %w", hs.Name, err)
			}
			resolved.Name = hs.Name
			resolved.Source = hs.Source
			if hs.Params != nil {
				resolved.Params = hs.Params
			}
			services = append(services, *resolved)
		} else {
			services = append(services, convertHCLService(hs))
		}
		serviceByName[hs.Name] = true
	}

	return services, nil
}

func convertHCLService(hs hclServiceRaw) service.Service {
	s := service.Service{
		Name:   hs.Name,
		Params: hs.Params,
	}
	if len(hs.Start) > 0 {
		s.Start = hs.Start[0]
	}
	if len(hs.Stop) > 0 {
		s.Stop = hs.Stop[0]
	}
	if len(hs.ReadyCheck) > 0 {
		rc := hs.ReadyCheck[0]
		s.ReadyCheck = &service.ReadyCheck{
			RunnerCommand: utils.RunnerCommand{
				Runner: rc.Runner,
				Args:   rc.Args,
				Params: rc.Params,
			},
			Interval: rc.Interval,
			Timeout:  rc.Timeout,
		}
	}
	return s
}

// hclVariableRaw is a minimal struct for parsing variable blocks from raw HCL.
type hclVariableRaw struct {
	Name   string          `hcl:"name,label"`
	Type   string          `hcl:"type"`
	Secret *VariableSecret `hcl:"secret,block"`
	Remain hcl.Body        `hcl:",remain"`
}

// hclPipelineVariables is a minimal struct for parsing only variable blocks from raw HCL.
type hclPipelineVariables struct {
	Variables []hclVariableRaw `hcl:"variable,block"`
	Remain    hcl.Body         `hcl:",remain"`
}

// ParseSecretVarsFromRaw parses secret-backed variable declarations from raw pipeline HCL.
// Used to populate the SecretVars field on pipelines loaded from the database.
func ParseSecretVarsFromRaw(raw []byte, vars map[string]interface{}) (map[string]VariableSecret, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	ectx := TypeEvalContext()

	var pv hclPipelineVariables
	err := hclsimple.Decode("pipeline.hcl", raw, ectx, &pv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse variables from raw HCL: %w", err)
	}

	secretVars := make(map[string]VariableSecret)
	for _, v := range pv.Variables {
		if v.Secret != nil {
			// Only include if not overridden by vars file
			if _, overridden := vars[v.Name]; !overridden {
				secretVars[v.Name] = *v.Secret
			}
		}
	}

	if len(secretVars) == 0 {
		return nil, nil
	}
	return secretVars, nil
}

// buildVarEvalContext parses variable declarations from raw HCL and builds
// an hcl.EvalContext with their default values, so that other blocks
// (e.g. service_type) referencing var.* can be decoded.
func buildVarEvalContext(raw []byte) (*hcl.EvalContext, error) {
	typeCtx := TypeEvalContext()

	var pvars Variables
	if err := hclsimple.Decode("pipeline.hcl", raw, typeCtx, &pvars); err != nil {
		return nil, fmt.Errorf("failed to parse variables: %w", err)
	}

	ecvars := make(map[string]cty.Value)
	for _, v := range pvars.Variables {
		if v.Secret != nil {
			placeholder := fmt.Sprintf("__pikoci_secret:%s:%s:%s__",
				v.Secret.Type, v.Secret.Path, v.Secret.Key)
			ecvars[v.Name] = cty.StringVal(placeholder)
			continue
		}
		a, ok := v.Default.(*hcl.Attribute)
		if !ok {
			switch v.Type {
			case "number":
				ecvars[v.Name] = cty.NumberIntVal(0)
			case "bool":
				ecvars[v.Name] = cty.False
			default:
				ecvars[v.Name] = cty.StringVal("")
			}
			continue
		}
		ctyv, diags := a.Expr.Value(typeCtx)
		if diags.HasErrors() {
			switch v.Type {
			case "number":
				ecvars[v.Name] = cty.NumberIntVal(0)
			case "bool":
				ecvars[v.Name] = cty.False
			default:
				ecvars[v.Name] = cty.StringVal("")
			}
			continue
		}
		ecvars[v.Name] = ctyv
	}

	// TODO: include HCL standard functions (format, join, etc.) once
	// hclFunctions() is extracted from the pikoci package to avoid a
	// circular import. For now, service blocks using HCL functions in
	// attribute values will fail to parse.
	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(ecvars),
		},
	}, nil
}
