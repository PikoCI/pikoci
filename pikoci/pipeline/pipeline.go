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
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pikoci/pikoci/pikoci/builtin"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/service"
	"github.com/pikoci/pikoci/pikoci/source"
	"github.com/pikoci/pikoci/pikoci/team"
	"github.com/pikoci/pikoci/pikoci/utils"
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
	NotificationTypes []notiftype.NotificationType    `json:"notification_types" hcl:"notification_type,block"`
	Notifications     []notification.Notification      `json:"notifications" hcl:"notification,block"`
	SecretVars        map[string]VariableSecret        `json:"secret_vars,omitempty"`
	Remain            hcl.Body                         `json:"-" hcl:",remain"`
	Raw               []byte                           `json:"raw"`
	LastBuildAt       *time.Time                       `json:"last_build_at,omitempty"`
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

// VariablesBasic is like Variables but does NOT decode secret block internals.
// This is needed because secret blocks may contain expressions (path = var.x)
// that reference variables not yet in the eval context during initial parsing.
type VariablesBasic struct {
	Variables []VariableBasic `hcl:"variable,block"`
	Remain    hcl.Body        `hcl:",remain"`
}

// VariableBasic is like Variable but without secret block decoding.
// Secret blocks go into Remain and are parsed separately from the AST.
type VariableBasic struct {
	Name    string      `hcl:"name,label"`
	Type    string      `hcl:"type"`
	Default interface{} `hcl:"default,optional"`
	Remain  hcl.Body    `hcl:",remain"`
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

// NotificationType looks up a notification type by name in the pipeline's configured
// notification types, falling back to built-in notification types. It returns the
// notification type and true if found, or an empty value and false otherwise.
func (pp *Pipeline) NotificationType(ntn string) (notiftype.NotificationType, bool) {
	for _, nt := range pp.NotificationTypes {
		if nt.Name == ntn {
			return nt, true
		}
	}

	if bnt, ok := builtin.NotificationTypes()[ntn]; ok {
		return bnt, true
	}

	return notiftype.NotificationType{}, false
}

// Notification looks up a notification by its canonical identifier. It returns the
// notification and true if found, or an empty value and false otherwise.
func (pp *Pipeline) Notification(nCan string) (notification.Notification, bool) {
	for _, n := range pp.Notifications {
		if n.Canonical == nCan {
			return n, true
		}
	}

	return notification.Notification{}, false
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

// hclPipelineVariables is a minimal struct for parsing only variable blocks from raw HCL.
// Uses VariableBasic (no secret block decoding) so path = var.x doesn't fail.
type hclPipelineVariables struct {
	Variables []VariableBasic `hcl:"variable,block"`
	Remain    hcl.Body        `hcl:",remain"`
}

// ParseSecretBlocksFromASTLenient is like ParseSecretBlocksFromAST but uses
// per-attribute fallback: if a path/key expression fails to evaluate (e.g.,
// because it references var.* not yet in scope), the field is left empty
// instead of returning an error. This is used for pass 1 bootstrapping.
func ParseSecretBlocksFromASTLenient(raw []byte, ectx *hcl.EvalContext) (map[string]VariableSecret, error) {
	file, diags := hclsyntax.ParseConfig(raw, "pipeline.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse pipeline HCL: %s", diags.Error())
	}

	result := make(map[string]VariableSecret)
	for _, block := range file.Body.(*hclsyntax.Body).Blocks {
		if block.Type != "variable" || len(block.Labels) == 0 {
			continue
		}
		varName := block.Labels[0]
		for _, sub := range block.Body.Blocks {
			if sub.Type != "secret" || len(sub.Labels) == 0 {
				continue
			}
			sv := VariableSecret{Type: sub.Labels[0]}
			if attr, exists := sub.Body.Attributes["path"]; exists {
				if val, vd := attr.Expr.Value(ectx); !vd.HasErrors() && val.Type() == cty.String {
					sv.Path = val.AsString()
				}
			}
			if attr, exists := sub.Body.Attributes["key"]; exists {
				if val, vd := attr.Expr.Value(ectx); !vd.HasErrors() && val.Type() == cty.String {
					sv.Key = val.AsString()
				}
			}
			result[varName] = sv
			break
		}
	}
	return result, nil
}

// ParseSecretBlocksFromAST extracts secret block information from raw HCL using
// AST traversal. This is needed because secret block path/key may contain
// variable expressions (e.g., path = var.x) that can't be decoded via struct
// tags when var.* isn't in the eval context. The ectx must be non-nil and
// should contain the full var.* namespace for expression evaluation.
func ParseSecretBlocksFromAST(raw []byte, ectx *hcl.EvalContext) (map[string]VariableSecret, error) {
	file, diags := hclsyntax.ParseConfig(raw, "pipeline.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse pipeline HCL: %s", diags.Error())
	}

	result := make(map[string]VariableSecret)
	for _, block := range file.Body.(*hclsyntax.Body).Blocks {
		if block.Type != "variable" || len(block.Labels) == 0 {
			continue
		}
		varName := block.Labels[0]
		for _, sub := range block.Body.Blocks {
			if sub.Type != "secret" || len(sub.Labels) == 0 {
				continue
			}
			sv := VariableSecret{Type: sub.Labels[0]}

			if attr, exists := sub.Body.Attributes["path"]; exists {
				val, vdiags := attr.Expr.Value(ectx)
				if vdiags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate secret path for variable %q: %s", varName, vdiags.Error())
				}
				if val.Type() != cty.String {
					return nil, fmt.Errorf("secret path for variable %q must be a string, got %s", varName, val.Type().FriendlyName())
				}
				sv.Path = val.AsString()
			}
			if attr, exists := sub.Body.Attributes["key"]; exists {
				val, vdiags := attr.Expr.Value(ectx)
				if vdiags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate secret key for variable %q: %s", varName, vdiags.Error())
				}
				if val.Type() != cty.String {
					return nil, fmt.Errorf("secret key for variable %q must be a string, got %s", varName, val.Type().FriendlyName())
				}
				sv.Key = val.AsString()
			}
			result[varName] = sv
			break
		}
	}
	return result, nil
}

// ParseSecretVarsFromRaw parses secret-backed variable declarations from raw pipeline HCL.
// Used to populate the SecretVars field on pipelines loaded from the database.
func ParseSecretVarsFromRaw(raw []byte, vars map[string]interface{}) (map[string]VariableSecret, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// Build eval context with all variable defaults/placeholders so that
	// path/key expressions like `path = var.x` can be resolved.
	varEctx, err := buildVarEvalContextWithOverrides(raw, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to build eval context for secret parsing: %w", err)
	}

	// Parse secret blocks from AST using the full eval context.
	secretVars, err := ParseSecretBlocksFromAST(raw, varEctx)
	if err != nil {
		return nil, err
	}

	// Exclude variables overridden by vars file
	for varName := range secretVars {
		if _, overridden := vars[varName]; overridden {
			delete(secretVars, varName)
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

	// Use hclPipelineVariables (which doesn't decode secret internals) so
	// that secret blocks with path = var.x don't cause decode failures.
	var pv hclPipelineVariables
	if err := hclsimple.Decode("pipeline.hcl", raw, typeCtx, &pv); err != nil {
		return nil, fmt.Errorf("failed to parse variables: %w", err)
	}

	// Detect secret blocks from AST using the lenient parser. Expression
	// evaluation uses TypeEvalContext (no var.*), so expressions referencing
	// var.* fall back to empty strings — the exact path/key are not needed
	// for the eval context, only that the variable produces a string placeholder.
	secretBlocks, err := ParseSecretBlocksFromASTLenient(raw, typeCtx)
	if err != nil {
		return nil, err
	}

	// Now parse default values for non-secret variables
	ecvars := make(map[string]cty.Value)
	for _, v := range pv.Variables {
		if _, isSecret := secretBlocks[v.Name]; isSecret {
			sv := secretBlocks[v.Name]
			placeholder := fmt.Sprintf("__pikoci_secret:%s:%s:%s__",
				sv.Type, sv.Path, sv.Key)
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
		ctyv, evalDiags := a.Expr.Value(typeCtx)
		if evalDiags.HasErrors() {
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

// buildVarEvalContextWithOverrides is like buildVarEvalContext but applies
// vars file overrides, so that overridden variables resolve to their override
// value instead of a secret placeholder.
func buildVarEvalContextWithOverrides(raw []byte, vars map[string]interface{}) (*hcl.EvalContext, error) {
	ectx, err := buildVarEvalContext(raw)
	if err != nil {
		return nil, err
	}
	if len(vars) == 0 {
		return ectx, nil
	}

	// Extract the current var object and override with provided values
	varObj := ectx.Variables["var"]
	ecvars := varObj.AsValueMap()
	if ecvars == nil {
		ecvars = make(map[string]cty.Value)
	}
	for k, v := range vars {
		switch val := v.(type) {
		case string:
			ecvars[k] = cty.StringVal(val)
		case float64:
			ecvars[k] = cty.NumberFloatVal(val)
		case bool:
			ecvars[k] = cty.BoolVal(val)
		default:
			return nil, fmt.Errorf("unsupported override type %T for variable %q", v, k)
		}
	}
	ectx.Variables["var"] = cty.ObjectVal(ecvars)
	return ectx, nil
}
