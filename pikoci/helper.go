package pikoci

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsimple"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pikoci/pikoci/pikoci/job"
	"github.com/pikoci/pikoci/pikoci/notification"
	"github.com/pikoci/pikoci/pikoci/notiftype"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/resource"
	"github.com/pikoci/pikoci/pikoci/restype"
	"github.com/pikoci/pikoci/pikoci/runner"
	"github.com/pikoci/pikoci/pikoci/sectype"
	"github.com/pikoci/pikoci/pikoci/service"
	"github.com/pikoci/pikoci/pikoci/source"
	"github.com/pikoci/pikoci/pikoci/utils"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
	"github.com/zclconf/go-cty/cty/gocty"
)

// hclGetStep is the HCL-decoded get step with per-step hooks.
type hclGetStep struct {
	Type     string   `json:"type" hcl:"type,label"`
	Name     string   `json:"name" hcl:"name,label"`
	Passed   []string `json:"passed" hcl:"passed,optional"`
	Trigger  bool     `json:"trigger" hcl:"trigger,optional"`
	Timeout  string   `json:"timeout" hcl:"timeout,optional"`
	Attempts int      `json:"attempts" hcl:"attempts,optional"`

	// Remain absorbs hook blocks (on_success, on_failure, on_cancel, ensure) so hclsimple.Decode
	// doesn't reject them. Hooks are parsed from the raw AST by parseHooks instead,
	// which supports both labeled (runner) and unlabeled (put) hook blocks.
	Remain hcl.Body `hcl:",remain"`
}

// hclTaskStep is the HCL-decoded task step with per-step hooks.
type hclTaskStep struct {
	Name     string              `json:"name" hcl:"name,label"`
	Timeout  string              `json:"timeout" hcl:"timeout,optional"`
	Attempts int                 `json:"attempts" hcl:"attempts,optional"`
	Inputs   []string            `json:"inputs" hcl:"inputs,optional"`
	Outputs  []string            `json:"outputs" hcl:"outputs,optional"`
	Run      utils.RunnerCommand `json:"run" hcl:"run,block"`

	Remain hcl.Body `hcl:",remain"` // absorbs hook blocks; parsed by parseHooks from AST
}

// hclPutStep is the HCL-decoded put step.
// Uses hcl.Body remain to absorb both params (attributes) and hook blocks,
// since map[string]string remain can only absorb attributes, not blocks.
// Params and hooks are both extracted from the raw AST in parseJobPlans.
type hclPutStep struct {
	Type     string `hcl:"type,label"`
	Name     string `hcl:"name,label"`
	Timeout  string `hcl:"timeout,optional"`
	Attempts int    `hcl:"attempts,optional"`

	Remain hcl.Body `hcl:",remain"`
}

// hclJob is the intermediate HCL-decoded job with separate get/task/put/notify arrays.
type hclJob struct {
	Name         string           `hcl:"name,label"`
	Concurrency  int              `hcl:"concurrency,optional"`
	SerialGroups []string         `hcl:"serial_groups,optional"`
	Timeout      string           `hcl:"timeout,optional"`
	Get          []hclGetStep     `hcl:"get,block"`
	Task         []hclTaskStep    `hcl:"task,block"`
	Put          []hclPutStep     `hcl:"put,block"`
	Notify       []hclNotifyStep  `hcl:"notify,block"`
	Service      []hclServiceRef  `hcl:"service,block"`

	Remain hcl.Body `hcl:",remain"` // absorbs hook blocks; parsed by parseHooks from AST
}

// hclResourceType is an intermediate struct that allows optional check/pull/push blocks
// when source is provided.
type hclResourceType struct {
	Name   string   `json:"name" hcl:"name,label"`
	Source string   `json:"source,omitempty" hcl:"source,optional"`
	Params []string `json:"params" hcl:"params,optional"`
	Cache  bool     `json:"cache" hcl:"cache,optional"`

	Check  []utils.RunnerCommand  `json:"check" hcl:"check,block"`
	Pull   []utils.RunnerCommand  `json:"pull" hcl:"pull,block"`
	Push   []utils.RunnerCommand  `json:"push" hcl:"push,block"`
	Runner []utils.RunnerOverride `json:"runner" hcl:"runner,block"`
}

func (hrt hclResourceType) toResourceType() restype.ResourceType {
	rt := restype.ResourceType{
		Name:   hrt.Name,
		Source: hrt.Source,
		Params: hrt.Params,
		Cache:  hrt.Cache,
	}
	if len(hrt.Check) > 0 {
		rt.Check = &hrt.Check[0]
	}
	if len(hrt.Pull) > 0 {
		rt.Pull = &hrt.Pull[0]
	}
	if len(hrt.Push) > 0 {
		rt.Push = &hrt.Push[0]
	}
	if len(hrt.Runner) > 0 {
		rt.Runner = &hrt.Runner[0]
	}
	return rt
}

// hclRunnerDef is an intermediate struct that allows optional run block
// when source is provided.
type hclRunnerDef struct {
	Name   string             `json:"name" hcl:"name,label"`
	Source string             `json:"source,omitempty" hcl:"source,optional"`
	Run    []utils.RunCommand `json:"run" hcl:"run,block"`
}

func (hrd hclRunnerDef) toRunner() runner.Runner {
	ru := runner.Runner{
		Name:   hrd.Name,
		Source: hrd.Source,
	}
	if len(hrd.Run) > 0 {
		ru.Run = hrd.Run[0]
	}
	return ru
}

// hclSecretType is an intermediate struct that allows optional get block
// when source is provided. Config attributes (address, token, etc.) are
// captured via Remain.
type hclSecretType struct {
	Name   string   `json:"name" hcl:"name,label"`
	Source string   `json:"source,omitempty" hcl:"source,optional"`
	Params []string `json:"params" hcl:"params,optional"`

	Get    []utils.RunnerCommand  `json:"get" hcl:"get,block"`
	Runner []utils.RunnerOverride `json:"runner" hcl:"runner,block"`
	Remain hcl.Body               `hcl:",remain"`
}

func (hst hclSecretType) toSecretType() sectype.SecretType {
	st := sectype.SecretType{
		Name:   hst.Name,
		Source: hst.Source,
		Params: hst.Params,
	}
	if len(hst.Get) > 0 {
		st.Get = hst.Get[0]
	}
	if len(hst.Runner) > 0 {
		st.Runner = &hst.Runner[0]
	}
	return st
}

// hclReadyCheck is the HCL-decoded ready_check block for a service.
type hclReadyCheck struct {
	Runner   string            `hcl:"runner,label"`
	Args     []string          `hcl:"args,optional"`
	Interval string            `hcl:"interval,optional"`
	Timeout  string            `hcl:"timeout,optional"`
	Params   map[string]string `hcl:",remain"`
}

// hclService is the HCL-decoded top-level service block.
type hclService struct {
	Name       string                 `hcl:"name,label"`
	Source     string                 `hcl:"source,optional"`
	Params     []string               `hcl:"params,optional"`
	Start      []utils.RunnerCommand  `hcl:"start,block"`
	ReadyCheck []hclReadyCheck        `hcl:"ready_check,block"`
	Stop       []utils.RunnerCommand  `hcl:"stop,block"`
	Runner     []utils.RunnerOverride `hcl:"runner,block"`
}

func (hs hclService) toService() service.Service {
	s := service.Service{
		Name:   hs.Name,
		Source: hs.Source,
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
	if len(hs.Runner) > 0 {
		s.Runner = &hs.Runner[0]
	}
	return s
}

// hclNotificationType is an intermediate struct that allows optional notify block
// when source is provided.
type hclNotificationType struct {
	Name   string   `json:"name" hcl:"name,label"`
	Source string   `json:"source,omitempty" hcl:"source,optional"`
	Params []string `json:"params" hcl:"params,optional"`

	Notify []utils.RunnerCommand  `json:"notify" hcl:"notify,block"`
	Runner []utils.RunnerOverride `json:"runner" hcl:"runner,block"`
}

func (hnt hclNotificationType) toNotificationType() notiftype.NotificationType {
	nt := notiftype.NotificationType{
		Name:   hnt.Name,
		Source: hnt.Source,
		Params: hnt.Params,
	}
	if len(hnt.Notify) > 0 {
		nt.Notify = &hnt.Notify[0]
	}
	if len(hnt.Runner) > 0 {
		nt.Runner = &hnt.Runner[0]
	}
	return nt
}

// hclNotifyStep is the HCL-decoded notify step.
type hclNotifyStep struct {
	Type    string `hcl:"type,label"`
	Name    string `hcl:"name,label"`
	Message string `hcl:"message,optional"`

	Remain hcl.Body `hcl:",remain"`
}

// hclServiceRef is a service reference inside a job block.
// Only param overrides are allowed (via Remain), no inline start/stop.
type hclServiceRef struct {
	Name   string   `hcl:"name,label"`
	Remain hcl.Body `hcl:",remain"`
}

// hclPipeline is the intermediate HCL-decoded pipeline.
type hclPipeline struct {
	Name              string                        `json:"name"`
	Jobs              []hclJob                      `hcl:"job,block"`
	Resources         []resource.Resource           `hcl:"resource,block"`
	ResourceTypes     []hclResourceType             `hcl:"resource_type,block"`
	NotificationTypes []hclNotificationType         `hcl:"notification_type,block"`
	Notifications     []notification.Notification   `hcl:"notification,block"`
	Runners           []hclRunnerDef                `hcl:"runner_type,block"`
	SecretTypes       []hclSecretType               `hcl:"secret_type,block"`
	Services          []hclService                  `hcl:"service_type,block"`
	Remain            hcl.Body                      `hcl:",remain"`
}

// hclFunctions returns the set of built-in HCL functions available in pipeline
// configuration files, including string manipulation, collection operations,
// numeric functions, encoding utilities, and regex functions.
func hclFunctions() map[string]function.Function {
	return map[string]function.Function{
		// String
		"chomp":       stdlib.ChompFunc,
		"format":      stdlib.FormatFunc,
		"formatlist":  stdlib.FormatListFunc,
		"indent":      stdlib.IndentFunc,
		"join":        stdlib.JoinFunc,
		"lower":       stdlib.LowerFunc,
		"replace":     stdlib.ReplaceFunc,
		"split":       stdlib.SplitFunc,
		"strlen":      stdlib.StrlenFunc,
		"strrev":      stdlib.ReverseFunc,
		"substr":      stdlib.SubstrFunc,
		"title":       stdlib.TitleFunc,
		"trim":        stdlib.TrimFunc,
		"trimprefix":  stdlib.TrimPrefixFunc,
		"trimsuffix":  stdlib.TrimSuffixFunc,
		"trimspace":   stdlib.TrimSpaceFunc,
		"upper":       stdlib.UpperFunc,
		"startswith":  startswithFunc,
		"endswith":    endswithFunc,
		"strcontains": strcontainsFunc,
		// Collection
		"chunklist":    stdlib.ChunklistFunc,
		"coalesce":     stdlib.CoalesceFunc,
		"coalescelist": stdlib.CoalesceListFunc,
		"compact":      stdlib.CompactFunc,
		"concat":       stdlib.ConcatFunc,
		"contains":     stdlib.ContainsFunc,
		"distinct":     stdlib.DistinctFunc,
		"element":      stdlib.ElementFunc,
		"flatten":      stdlib.FlattenFunc,
		"keys":         stdlib.KeysFunc,
		"length":       stdlib.LengthFunc,
		"lookup":       stdlib.LookupFunc,
		"merge":        stdlib.MergeFunc,
		"one":          oneFunc,
		"range":        stdlib.RangeFunc,
		"reverse":      stdlib.ReverseListFunc,
		"slice":        stdlib.SliceFunc,
		"sort":         stdlib.SortFunc,
		"values":       stdlib.ValuesFunc,
		"zipmap":       stdlib.ZipmapFunc,
		"alltrue":      alltrueFunc,
		"anytrue":      anytrueFunc,
		"sum":          sumFunc,
		"transpose":    transposeFunc,
		// Numeric
		"abs":     stdlib.AbsoluteFunc,
		"ceil":    stdlib.CeilFunc,
		"floor":   stdlib.FloorFunc,
		"log":     stdlib.LogFunc,
		"max":     stdlib.MaxFunc,
		"min":     stdlib.MinFunc,
		"parseint": stdlib.ParseIntFunc,
		"pow":     stdlib.PowFunc,
		"signum":  stdlib.SignumFunc,
		// Encoding
		"jsonencode":  stdlib.JSONEncodeFunc,
		"jsondecode":  stdlib.JSONDecodeFunc,
		"csvdecode":   stdlib.CSVDecodeFunc,
		"base64encode": base64encodeFunc,
		"base64decode": base64decodeFunc,
		"urlencode":    urlencodeFunc,
		// Date/Time
		"formatdate": stdlib.FormatDateFunc,
		"timeadd":    stdlib.TimeAddFunc,
		"timestamp":  timestampFunc,
		// Regex
		"regex":        stdlib.RegexFunc,
		"regexall":     stdlib.RegexAllFunc,
		"regexreplace": stdlib.RegexReplaceFunc,
		// Set
		"toset":           tosetFunc,
		"setproduct":      stdlib.SetProductFunc,
		"setintersection": stdlib.SetIntersectionFunc,
		"setunion":        stdlib.SetUnionFunc,
		"setsubtract":              stdlib.SetSubtractFunc,
		"setsymmetricdifference":   stdlib.SetSymmetricDifferenceFunc,
		// Type conversion
		"tostring": makeToFunc(cty.String),
		"tonumber": makeToFunc(cty.Number),
		"tobool":   makeToFunc(cty.Bool),
		"tolist":   makeToFunc(cty.List(cty.DynamicPseudoType)),
		"tomap":    makeToFunc(cty.Map(cty.DynamicPseudoType)),
	}
}

// tosetFunc converts a list of strings to a set (deduplicated list) of strings.
var tosetFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{
			Name: "list",
			Type: cty.List(cty.String),
		},
	},
	Type: function.StaticReturnType(cty.Set(cty.String)),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		list := args[0]
		if !list.IsKnown() {
			return cty.UnknownVal(cty.Set(cty.String)), nil
		}
		if list.LengthInt() == 0 {
			return cty.SetValEmpty(cty.String), nil
		}
		var vals []cty.Value
		for it := list.ElementIterator(); it.Next(); {
			_, v := it.Element()
			vals = append(vals, v)
		}
		return cty.SetVal(vals), nil
	},
})

var startswithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "prefix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() || !args[1].IsKnown() {
			return cty.UnknownVal(cty.Bool), nil
		}
		return cty.BoolVal(strings.HasPrefix(args[0].AsString(), args[1].AsString())), nil
	},
})

var endswithFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "suffix", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() || !args[1].IsKnown() {
			return cty.UnknownVal(cty.Bool), nil
		}
		return cty.BoolVal(strings.HasSuffix(args[0].AsString(), args[1].AsString())), nil
	},
})

var strcontainsFunc = function.New(&function.Spec{
	Params: []function.Parameter{
		{Name: "str", Type: cty.String},
		{Name: "substr", Type: cty.String},
	},
	Type: function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() || !args[1].IsKnown() {
			return cty.UnknownVal(cty.Bool), nil
		}
		return cty.BoolVal(strings.Contains(args[0].AsString(), args[1].AsString())), nil
	},
})

var base64encodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(cty.String), nil
		}
		return cty.StringVal(base64.StdEncoding.EncodeToString([]byte(args[0].AsString()))), nil
	},
})

var base64decodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(cty.String), nil
		}
		decoded, err := base64.StdEncoding.DecodeString(args[0].AsString())
		if err != nil {
			return cty.NilVal, fmt.Errorf("failed to decode base64: %w", err)
		}
		return cty.StringVal(string(decoded)), nil
	},
})

var urlencodeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "str", Type: cty.String}},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(cty.String), nil
		}
		return cty.StringVal(url.QueryEscape(args[0].AsString())), nil
	},
})

var timestampFunc = function.New(&function.Spec{
	Params: []function.Parameter{},
	Type:   function.StaticReturnType(cty.String),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		return cty.StringVal(time.Now().UTC().Format(time.RFC3339)), nil
	},
})

var alltrueFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Bool)}},
	Type:   function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(cty.Bool), nil
		}
		for it := args[0].ElementIterator(); it.Next(); {
			_, v := it.Element()
			if v.False() {
				return cty.False, nil
			}
		}
		return cty.True, nil
	},
})

var anytrueFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Bool)}},
	Type:   function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(cty.Bool), nil
		}
		for it := args[0].ElementIterator(); it.Next(); {
			_, v := it.Element()
			if v.True() {
				return cty.True, nil
			}
		}
		return cty.False, nil
	},
})

var oneFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.DynamicPseudoType}},
	Type: func(args []cty.Value) (cty.Type, error) {
		ty := args[0].Type()
		switch {
		case ty.IsListType():
			return ty.ElementType(), nil
		case ty.IsSetType():
			return ty.ElementType(), nil
		case ty.IsTupleType():
			etys := ty.TupleElementTypes()
			if len(etys) == 0 {
				return cty.DynamicPseudoType, nil
			}
			return etys[0], nil
		default:
			return cty.NilType, fmt.Errorf("argument must be a list, set, or tuple")
		}
	},
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		val := args[0]
		if !val.IsKnown() {
			return cty.UnknownVal(retType), nil
		}
		l := val.LengthInt()
		if l == 0 {
			return cty.NullVal(retType), nil
		}
		if l != 1 {
			return cty.NilVal, fmt.Errorf("must be a single element collection, got %d", l)
		}
		it := val.ElementIterator()
		it.Next()
		_, v := it.Element()
		return v, nil
	},
})

var sumFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Number)}},
	Type:   function.StaticReturnType(cty.Number),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(cty.Number), nil
		}
		if args[0].LengthInt() == 0 {
			return cty.NumberIntVal(0), nil
		}
		s := new(big.Float)
		for it := args[0].ElementIterator(); it.Next(); {
			_, v := it.Element()
			s.Add(s, v.AsBigFloat())
		}
		return cty.NumberVal(s), nil
	},
})

var transposeFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "values", Type: cty.Map(cty.List(cty.String))}},
	Type:   function.StaticReturnType(cty.Map(cty.List(cty.String))),
	Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
		if !args[0].IsKnown() {
			return cty.UnknownVal(retType), nil
		}
		result := make(map[string][]string)
		for it := args[0].ElementIterator(); it.Next(); {
			k, v := it.Element()
			key := k.AsString()
			for vit := v.ElementIterator(); vit.Next(); {
				_, elem := vit.Element()
				val := elem.AsString()
				result[val] = append(result[val], key)
			}
		}
		if len(result) == 0 {
			return cty.MapValEmpty(cty.List(cty.String)), nil
		}
		m := make(map[string]cty.Value, len(result))
		for k, v := range result {
			vals := make([]cty.Value, len(v))
			for i, s := range v {
				vals[i] = cty.StringVal(s)
			}
			m[k] = cty.ListVal(vals)
		}
		return cty.MapVal(m), nil
	},
})

// makeToFunc wraps stdlib.MakeToFunc to create type conversion functions.
func makeToFunc(wantTy cty.Type) function.Function {
	return stdlib.MakeToFunc(wantTy)
}

// readPipeline parses raw HCL pipeline configuration bytes into a Pipeline
// struct. It handles variable resolution (string, number, bool, and secret
// types), source resolution for resource types, runners, secret types, and
// services, and extracts ordered job plans from the HCL AST.
func (q *PikoCI) readPipeline(ctx context.Context, rpp []byte, vars map[string]interface{}) (*pipeline.Pipeline, error) {
	funcs := hclFunctions()
	ectx := pipeline.TypeEvalContext()
	ectx.Functions = funcs
	var pvars pipeline.Variables
	err := hclsimple.Decode("pipeline.hcl", rpp, ectx, &pvars)
	if err != nil {
		return nil, fmt.Errorf("failed to Decode Pipeline config: %w", err)
	}

	ecvars := make(map[string]cty.Value)
	secretVars := make(map[string]pipeline.VariableSecret)
	for _, v := range pvars.Variables {
		switch v.Type {
		case "string":
			if mv, ok := vars[v.Name]; ok {
				s, ok := mv.(string)
				if !ok {
					return nil, fmt.Errorf("variable %q configured with invalid type type, expected 'string'", v.Name)
				}
				ecvars[v.Name] = cty.StringVal(s)
			} else if v.Secret != nil {
				placeholder := fmt.Sprintf("__pikoci_secret:%s:%s:%s__",
					v.Secret.Type, v.Secret.Path, v.Secret.Key)
				ecvars[v.Name] = cty.StringVal(placeholder)
				secretVars[v.Name] = *v.Secret
			} else {
				a, ok := v.Default.(*hcl.Attribute)
				if !ok {
					return nil, fmt.Errorf("variable %q has an invalid default type, expected 'string'", v.Name)
				}
				ctyv, _ := a.Expr.Value(ectx)
				var s string
				err = gocty.FromCtyValue(ctyv, &s)
				if err != nil {
					return nil, fmt.Errorf("variable %q has an invalid default type, expected 'string'", v.Name)
				}
				ecvars[v.Name] = cty.StringVal(s)
			}
		case "number":
			if mv, ok := vars[v.Name]; ok {
				n, ok := mv.(float64)
				if !ok {
					return nil, fmt.Errorf("variable %q configured with invalid type type, expected 'number'", v.Name)
				}
				ecvars[v.Name] = cty.NumberVal(big.NewFloat(n))
			} else {
				a, ok := v.Default.(*hcl.Attribute)
				if !ok {
					return nil, fmt.Errorf("variable %q has an invalid default type, expected 'number'", v.Name)
				}
				ctyv, _ := a.Expr.Value(ectx)
				var n float64
				err = gocty.FromCtyValue(ctyv, &n)
				if err != nil {
					return nil, fmt.Errorf("variable %q has an invalid default type, expected 'number'", v.Name)
				}
				ecvars[v.Name] = cty.NumberVal(big.NewFloat(n))
			}
		case "bool":
			if mv, ok := vars[v.Name]; ok {
				b, ok := mv.(bool)
				if !ok {
					return nil, fmt.Errorf("variable %q configured with invalid type type, expected 'bool'", v.Name)
				}
				ecvars[v.Name] = cty.BoolVal(b)
			} else {
				a, ok := v.Default.(*hcl.Attribute)
				if !ok {
					return nil, fmt.Errorf("variable %q has an invalid default type, expected 'bool'", v.Name)
				}
				ctyv, _ := a.Expr.Value(ectx)
				var b bool
				err = gocty.FromCtyValue(ctyv, &b)
				if err != nil {
					return nil, fmt.Errorf("variable %q has an invalid default type, expected 'bool'", v.Name)
				}
				ecvars[v.Name] = cty.BoolVal(b)
			}
		}
	}
	ectx = &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(ecvars),
		},
		Functions: funcs,
	}

	// Detect for_each/matrix job blocks before the main decode
	forEachExpansions, err := detectForEachExpansions(rpp, ectx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect for_each expansions: %w", err)
	}

	// Strip for_each job blocks from the HCL for the main decode
	decodeRPP, forEachBlockBytes := stripForEachJobBlocks(rpp, forEachExpansions)

	var hp hclPipeline
	err = hclsimple.Decode("pipeline.hcl", decodeRPP, ectx, &hp)
	if err != nil {
		return nil, fmt.Errorf("failed to Decode Pipeline config: %w", err)
	}

	// Decode for_each job instances and append to hp.Jobs
	type forEachJobMeta struct {
		baseName string
		key      string
		ectx     *hcl.EvalContext
	}
	forEachMetas := make(map[string]forEachJobMeta)
	cleanedForEachBlocks := make(map[string][]byte) // cache cleaned blocks per base name
	for _, exp := range forEachExpansions {
		blockRaw := forEachBlockBytes[exp.baseName]
		cleanBlock, cErr := removeForEachAndMatrixFromBlock(blockRaw)
		if cErr != nil {
			return nil, fmt.Errorf("failed to clean for_each block for job %q: %w", exp.baseName, cErr)
		}
		cleanedForEachBlocks[exp.baseName] = cleanBlock
		for _, inst := range exp.instances {
			instEctx := makeInstanceEctx(ectx, inst.eachVal)
			var singleJobFile struct {
				Jobs []hclJob `hcl:"job,block"`
			}
			dErr := hclsimple.Decode("job.hcl", cleanBlock, instEctx, &singleJobFile)
			if dErr != nil {
				return nil, fmt.Errorf("failed to decode for_each instance %q of job %q: %w", inst.key, exp.baseName, dErr)
			}
			if len(singleJobFile.Jobs) == 0 {
				return nil, fmt.Errorf("for_each instance %q of job %q produced no job", inst.key, exp.baseName)
			}
			expandedJob := singleJobFile.Jobs[0]
			expandedJob.Name = exp.baseName + "--" + inst.slugKey
			hp.Jobs = append(hp.Jobs, expandedJob)
			forEachMetas[expandedJob.Name] = forEachJobMeta{
				baseName: exp.baseName,
				key:      inst.key,
				ectx:     instEctx,
			}
		}
	}

	// Convert intermediate types and resolve sources
	var resourceTypes []restype.ResourceType
	for _, hrt := range hp.ResourceTypes {
		if hrt.Source != "" {
			hasInline := len(hrt.Check) > 0 || len(hrt.Pull) > 0 || len(hrt.Push) > 0
			if hasInline {
				return nil, fmt.Errorf("resource_type %q has both source and inline commands, which is not allowed", hrt.Name)
			}
			resolved, err := source.ResolveResourceType(ctx, hrt.Source)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve source for resource_type %q: %w", hrt.Name, err)
			}
			resolved.Name = hrt.Name
			resolved.Source = hrt.Source
			if hrt.Cache {
				resolved.Cache = true
			}
			if len(hrt.Runner) > 0 {
				resolved.Runner = &hrt.Runner[0]
			}
			resourceTypes = append(resourceTypes, *resolved)
		} else {
			resourceTypes = append(resourceTypes, hrt.toResourceType())
		}
	}

	var runners []runner.Runner
	for _, hrd := range hp.Runners {
		if hrd.Source != "" {
			hasInline := len(hrd.Run) > 0
			if hasInline {
				return nil, fmt.Errorf("runner_type %q has both source and inline commands, which is not allowed", hrd.Name)
			}
			resolved, err := source.ResolveRunner(ctx, hrd.Source)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve source for runner_type %q: %w", hrd.Name, err)
			}
			resolved.Name = hrd.Name
			resolved.Source = hrd.Source
			runners = append(runners, *resolved)
		} else {
			runners = append(runners, hrd.toRunner())
		}
	}

	// Parse secret_type config attributes from the raw HCL AST.
	// Known fields (name, source, params, get) are handled by hclsimple.
	// Any extra attributes are config values (address, token, etc.).
	knownSecretTypeAttrs := map[string]bool{"source": true, "params": true}
	secretTypeConfigs := make(map[int]map[string]string)
	{
		file, diags := hclsyntax.ParseConfig(rpp, "pipeline.hcl", hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			return nil, fmt.Errorf("failed to parse pipeline HCL: %s", diags.Error())
		}
		stIdx := 0
		for _, block := range file.Body.(*hclsyntax.Body).Blocks {
			if block.Type != "secret_type" {
				continue
			}
			config := make(map[string]string)
			for name, attr := range block.Body.Attributes {
				if knownSecretTypeAttrs[name] {
					continue
				}
				val, vdiags := attr.Expr.Value(ectx)
				if vdiags.HasErrors() {
					return nil, fmt.Errorf("failed to evaluate secret_type config %q: %s", name, vdiags.Error())
				}
				config[name] = val.AsString()
			}
			if len(config) > 0 {
				secretTypeConfigs[stIdx] = config
			}
			stIdx++
		}
	}

	var secretTypes []sectype.SecretType
	for i, hst := range hp.SecretTypes {
		if hst.Source != "" {
			hasInline := len(hst.Get) > 0
			if hasInline {
				return nil, fmt.Errorf("secret_type %q has both source and inline commands, which is not allowed", hst.Name)
			}
			resolved, err := source.ResolveSecretType(ctx, hst.Source)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve source for secret_type %q: %w", hst.Name, err)
			}
			resolved.Name = hst.Name
			resolved.Source = hst.Source
			if cfg, ok := secretTypeConfigs[i]; ok {
				resolved.Config = cfg
			}
			if len(hst.Runner) > 0 {
				resolved.Runner = &hst.Runner[0]
			}
			secretTypes = append(secretTypes, *resolved)
		} else {
			st := hst.toSecretType()
			if cfg, ok := secretTypeConfigs[i]; ok {
				st.Config = cfg
			}
			secretTypes = append(secretTypes, st)
		}
	}

	var services []service.Service
	for _, hs := range hp.Services {
		if hs.Source != "" {
			hasInline := len(hs.Start) > 0 || len(hs.Stop) > 0 || len(hs.ReadyCheck) > 0
			if hasInline {
				return nil, fmt.Errorf("service_type %q has both source and inline commands, which is not allowed", hs.Name)
			}
			resolved, err := source.ResolveService(ctx, hs.Source)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve source for service_type %q: %w", hs.Name, err)
			}
			resolved.Name = hs.Name
			resolved.Source = hs.Source
			if hs.Params != nil {
				resolved.Params = hs.Params
			}
			if len(hs.Runner) > 0 {
				resolved.Runner = &hs.Runner[0]
			}
			services = append(services, *resolved)
		} else {
			if len(hs.Start) == 0 {
				return nil, fmt.Errorf("service_type %q must have a start block", hs.Name)
			}
			if len(hs.Stop) == 0 {
				return nil, fmt.Errorf("service_type %q must have a stop block", hs.Name)
			}
			services = append(services, hs.toService())
		}
	}

	// Convert notification types and resolve sources
	var notificationTypes []notiftype.NotificationType
	for _, hnt := range hp.NotificationTypes {
		if hnt.Source != "" {
			hasInline := len(hnt.Notify) > 0
			if hasInline {
				return nil, fmt.Errorf("notification_type %q has both source and inline commands, which is not allowed", hnt.Name)
			}
			resolved, err := source.ResolveNotificationType(ctx, hnt.Source)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve source for notification_type %q: %w", hnt.Name, err)
			}
			resolved.Name = hnt.Name
			resolved.Source = hnt.Source
			if len(hnt.Runner) > 0 {
				resolved.Runner = &hnt.Runner[0]
			}
			notificationTypes = append(notificationTypes, *resolved)
		} else {
			notificationTypes = append(notificationTypes, hnt.toNotificationType())
		}
	}

	// Validate runner overrides: only exec commands can be overridden
	for _, rt := range resourceTypes {
		if rt.Runner == nil {
			continue
		}
		for _, cmd := range []*utils.RunnerCommand{rt.Check, rt.Pull, rt.Push} {
			if cmd != nil && cmd.Runner != "exec" {
				return nil, fmt.Errorf("resource_type %q has runner override but command uses non-exec runner %q", rt.Name, cmd.Runner)
			}
		}
	}
	for _, nt := range notificationTypes {
		if nt.Runner == nil {
			continue
		}
		if nt.Notify != nil && nt.Notify.Runner != "exec" {
			return nil, fmt.Errorf("notification_type %q has runner override but command uses non-exec runner %q", nt.Name, nt.Notify.Runner)
		}
	}
	for _, st := range secretTypes {
		if st.Runner == nil {
			continue
		}
		if st.Get.Runner != "" && st.Get.Runner != "exec" {
			return nil, fmt.Errorf("secret_type %q has runner override but command uses non-exec runner %q", st.Name, st.Get.Runner)
		}
	}
	for _, svc := range services {
		if svc.Runner == nil {
			continue
		}
		for _, cmd := range []utils.RunnerCommand{svc.Start, svc.Stop} {
			if cmd.Runner != "" && cmd.Runner != "exec" {
				return nil, fmt.Errorf("service_type %q has runner override but command uses non-exec runner %q", svc.Name, cmd.Runner)
			}
		}
		if svc.ReadyCheck != nil && svc.ReadyCheck.Runner != "exec" {
			return nil, fmt.Errorf("service_type %q has runner override but ready_check uses non-exec runner %q", svc.Name, svc.ReadyCheck.Runner)
		}
	}

	// Compute notification canonicals and validate
	notifications := hp.Notifications
	for i, n := range notifications {
		notifications[i].Canonical = utils.NotificationCanonical(n.Type, n.Name)
		// Validate on/jobs/exclude
		if len(n.Jobs) > 0 && len(n.Exclude) > 0 {
			return nil, fmt.Errorf("notification %q: jobs and exclude are mutually exclusive", notifications[i].Canonical)
		}
		if (len(n.Jobs) > 0 || len(n.Exclude) > 0) && len(n.On) == 0 {
			return nil, fmt.Errorf("notification %q: jobs/exclude requires on field", notifications[i].Canonical)
		}
		for _, ev := range n.On {
			switch ev {
			case "success", "failure", "cancel", "all":
			default:
				return nil, fmt.Errorf("notification %q: invalid on event %q (valid: success, failure, cancel, all)", notifications[i].Canonical, ev)
			}
		}
		if len(n.On) > 1 {
			for _, ev := range n.On {
				if ev == "all" {
					return nil, fmt.Errorf("notification %q: 'all' cannot be combined with other events", notifications[i].Canonical)
				}
			}
		}
	}

	// Build job block pairs: (hclJob, AST block, ectx) for parseJobPlans.
	// Regular jobs come from decodeRPP, for_each jobs from their block bytes.
	var jobPairs []jobBlockPair

	// Parse stripped HCL for regular job AST blocks
	if len(hp.Jobs) > 0 {
		regularFile, rDiags := hclsyntax.ParseConfig(decodeRPP, "pipeline.hcl", hcl.Pos{Line: 1, Column: 1})
		if rDiags.HasErrors() {
			return nil, fmt.Errorf("failed to parse stripped pipeline HCL: %s", rDiags.Error())
		}
		regularBody := regularFile.Body.(*hclsyntax.Body)
		regularJobIdx := 0
		for _, block := range regularBody.Blocks {
			if block.Type != "job" {
				continue
			}
			if regularJobIdx >= len(hp.Jobs)-len(forEachMetas) {
				break
			}
			hj := hp.Jobs[regularJobIdx]
			// Skip if this is a for_each expanded job (they're appended at the end)
			if _, isForEach := forEachMetas[hj.Name]; isForEach {
				continue
			}
			jobPairs = append(jobPairs, jobBlockPair{
				hj:      hj,
				block:   block,
				jobEctx: ectx,
			})
			regularJobIdx++
		}
	}

	// Parse for_each job blocks and add pairs for each instance
	for _, exp := range forEachExpansions {
		cleanBlock := cleanedForEachBlocks[exp.baseName]
		cleanFile, cDiags := hclsyntax.ParseConfig(cleanBlock, "job.hcl", hcl.Pos{Line: 1, Column: 1})
		if cDiags.HasErrors() {
			return nil, fmt.Errorf("failed to parse cleaned for_each block for job %q: %s", exp.baseName, cDiags.Error())
		}
		cleanBody := cleanFile.Body.(*hclsyntax.Body)
		var astBlock *hclsyntax.Block
		for _, b := range cleanBody.Blocks {
			if b.Type == "job" {
				astBlock = b
				break
			}
		}
		if astBlock == nil {
			return nil, fmt.Errorf("no job block found in cleaned for_each block for %q", exp.baseName)
		}

		// Find the expanded hclJobs for this base name
		for _, hj := range hp.Jobs {
			meta, ok := forEachMetas[hj.Name]
			if !ok || meta.baseName != exp.baseName {
				continue
			}
			jobPairs = append(jobPairs, jobBlockPair{
				hj:      hj,
				block:   astBlock,
				jobEctx: meta.ectx,
			})
		}
	}

	// Parse job plans using paired blocks
	jobPlans, jobHooksMap, expandedServices, err := parseJobPlansFromPairs(jobPairs, services)
	if err != nil {
		return nil, fmt.Errorf("failed to parse job plans: %w", err)
	}

	pp := pipeline.Pipeline{
		Resources:         hp.Resources,
		ResourceTypes:     resourceTypes,
		NotificationTypes: notificationTypes,
		Notifications:     notifications,
		Runners:           runners,
		SecretTypes:       secretTypes,
		Services:          expandedServices,
		SecretVars:        secretVars,
	}

	for _, hj := range hp.Jobs {
		if hj.Concurrency < 0 {
			return nil, fmt.Errorf("job %q: concurrency must be >= 0", hj.Name)
		}
		for _, sg := range hj.SerialGroups {
			if !utils.ValidateCanonical(sg) {
				return nil, fmt.Errorf("job %q: invalid serial_group name %q (must be lowercase alphanumeric with hyphens)", hj.Name, sg)
			}
		}
		var jobTimeout time.Duration
		if hj.Timeout != "" {
			jobTimeout, err = time.ParseDuration(hj.Timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid timeout %q on job %q: %w", hj.Timeout, hj.Name, err)
			}
		}
		jh := jobHooksMap[hj.Name]
		j := job.Job{
			Name:         hj.Name,
			Concurrency:  hj.Concurrency,
			SerialGroups: hj.SerialGroups,
			Timeout:      jobTimeout,
			Plan:         jobPlans[hj.Name],
			OnSuccess:    jh.OnSuccess,
			OnFailure:    jh.OnFailure,
			OnCancel:     jh.OnCancel,
			Ensure:       jh.Ensure,
		}
		if meta, ok := forEachMetas[hj.Name]; ok {
			j.ForEachGroup = meta.baseName
			j.ForEachKey = meta.key
		}
		pp.Jobs = append(pp.Jobs, j)
	}

	for i, r := range pp.Resources {
		pp.Resources[i].Canonical = utils.ResourceCanonical(r.Type, r.Name)
	}
	return &pp, nil
}

// jobBlockPair associates a decoded hclJob with its AST block and eval context.
type jobBlockPair struct {
	hj      hclJob
	block   *hclsyntax.Block
	jobEctx *hcl.EvalContext
}

// jobHooks holds parsed hook steps for a job.
type jobHooks struct {
	OnSuccess []job.HookStep
	OnFailure []job.HookStep
	OnCancel  []job.HookStep
	Ensure    []job.HookStep
}

// parseHooks finds all hook steps (runner commands and put blocks) inside a specific
// hook type (on_success, on_failure, on_cancel, ensure) within the given AST block.
// Labeled blocks (e.g. on_success "exec" { ... }) are runner commands.
// Unlabeled blocks (e.g. on_success { put "type" "name" { ... } }) contain put steps.
func parseHooks(block *hclsyntax.Block, ectx *hcl.EvalContext, hookType string) []job.HookStep {
	var steps []job.HookStep
	for _, b := range block.Body.Blocks {
		if b.Type != hookType {
			continue
		}

		if len(b.Labels) == 1 {
			// Labeled hook: runner command (e.g. on_success "exec" { args = [...] })
			rc := utils.RunnerCommand{
				Runner: b.Labels[0],
				Params: make(map[string]string),
			}
			for name, attr := range b.Body.Attributes {
				val, vdiags := attr.Expr.Value(ectx)
				if vdiags.HasErrors() {
					continue
				}
				if name == "args" {
					// Parse args as a list of strings
					if val.Type().IsListType() || val.Type().IsTupleType() {
						for it := val.ElementIterator(); it.Next(); {
							_, v := it.Element()
							if v.Type() == cty.String {
								rc.Args = append(rc.Args, v.AsString())
							}
						}
					}
				} else {
					rc.Params[name] = val.AsString()
				}
			}
			steps = append(steps, job.HookStep{
				Type:   job.StepTypeRunner,
				Runner: &rc,
			})
		} else if len(b.Labels) == 0 {
			// Unlabeled hook: contains put and/or notify blocks
			for _, inner := range b.Body.Blocks {
				if inner.Type == "put" && len(inner.Labels) == 2 {
					putType := inner.Labels[0]
					putName := inner.Labels[1]
					params := make(map[string]string)
					for name, attr := range inner.Body.Attributes {
						val, vdiags := attr.Expr.Value(ectx)
						if vdiags.HasErrors() {
							continue
						}
						params[name] = val.AsString()
					}
					steps = append(steps, job.HookStep{
						Type: job.StepTypePut,
						Put: &job.PutStep{
							Type:   putType,
							Name:   putName,
							Params: params,
						},
					})
				} else if inner.Type == "notify" && len(inner.Labels) == 2 {
					notifyType := inner.Labels[0]
					notifyName := inner.Labels[1]
					params := make(map[string]string)
					var message string
					for name, attr := range inner.Body.Attributes {
						val, vdiags := attr.Expr.Value(ectx)
						if vdiags.HasErrors() {
							continue
						}
						if name == "message" {
							message = val.AsString()
						} else {
							params[name] = val.AsString()
						}
					}
					steps = append(steps, job.HookStep{
						Type: job.StepTypeNotify,
						Notify: &job.NotifyStep{
							Type:    notifyType,
							Name:    notifyName,
							Params:  params,
							Message: message,
						},
					})
				}
			}
		}
	}
	return steps
}

// parseJobPlansFromPairs extracts ordered plan steps from pre-paired (hclJob,
// AST block, eval context) tuples. This supports both regular jobs and for_each
// expanded jobs where each instance has its own eval context.
func parseJobPlansFromPairs(pairs []jobBlockPair, services []service.Service) (map[string][]job.PlanStep, map[string]jobHooks, []service.Service, error) {
	result := make(map[string][]job.PlanStep)
	jhMap := make(map[string]jobHooks)

	for _, pair := range pairs {
		hj := pair.hj
		block := pair.block
		pairEctx := pair.jobEctx

		serviceByName := make(map[string]service.Service)
		for _, s := range services {
			serviceByName[s.Name] = s
		}

		var plan []job.PlanStep
		getIdx, taskIdx, putIdx, notifyIdx, serviceIdx := 0, 0, 0, 0, 0

		for _, innerBlock := range block.Body.Blocks {
			switch innerBlock.Type {
			case "matrix":
				// Skip matrix blocks (already processed during expansion)
				continue
			case "service":
				if serviceIdx >= len(hj.Service) {
					continue
				}
				sr := hj.Service[serviceIdx]
				serviceIdx++

				if _, ok := serviceByName[sr.Name]; !ok {
					return nil, nil, nil, fmt.Errorf("service_type %q referenced in job %q does not exist", sr.Name, hj.Name)
				}

				params := make(map[string]string)
				if sr.Remain != nil {
					if body, ok := sr.Remain.(*hclsyntax.Body); ok {
						for name, attr := range body.Attributes {
							val, vdiags := attr.Expr.Value(nil)
							if vdiags.HasErrors() {
								return nil, nil, nil, fmt.Errorf("failed to evaluate service param %q: %s", name, vdiags.Error())
							}
							params[name] = val.AsString()
						}
					}
				}

				var paramMap map[string]string
				if len(params) > 0 {
					paramMap = params
				}

				plan = append(plan, job.PlanStep{
					Type: job.StepTypeService,
					Service: &job.ServiceStep{
						Name:   sr.Name,
						Params: paramMap,
					},
				})
			case "get":
				if getIdx >= len(hj.Get) {
					continue
				}
				g := hj.Get[getIdx]
				getIdx++
				var timeout time.Duration
				if g.Timeout != "" {
					var err error
					timeout, err = time.ParseDuration(g.Timeout)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("invalid timeout %q on get step %q: %w", g.Timeout, g.Name, err)
					}
				}
				if g.Attempts < 0 {
					return nil, nil, nil, fmt.Errorf("invalid attempts %d on get step %q: must be >= 0", g.Attempts, g.Name)
				}
				plan = append(plan, job.PlanStep{
					Type:     job.StepTypeGet,
					Timeout:  timeout,
					Attempts: g.Attempts,
					Get: &job.GetStep{
						Type:    g.Type,
						Name:    g.Name,
						Passed:  g.Passed,
						Trigger: g.Trigger,
					},
					OnSuccess: parseHooks(innerBlock, pairEctx, "on_success"),
					OnFailure: parseHooks(innerBlock, pairEctx, "on_failure"),
					OnCancel:  parseHooks(innerBlock, pairEctx, "on_cancel"),
					Ensure:    parseHooks(innerBlock, pairEctx, "ensure"),
				})
			case "task":
				if taskIdx >= len(hj.Task) {
					continue
				}
				t := hj.Task[taskIdx]
				taskIdx++
				var timeout time.Duration
				if t.Timeout != "" {
					var err error
					timeout, err = time.ParseDuration(t.Timeout)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("invalid timeout %q on task step %q: %w", t.Timeout, t.Name, err)
					}
				}
				if t.Attempts < 0 {
					return nil, nil, nil, fmt.Errorf("invalid attempts %d on task step %q: must be >= 0", t.Attempts, t.Name)
				}
				plan = append(plan, job.PlanStep{
					Type:     job.StepTypeTask,
					Timeout:  timeout,
					Attempts: t.Attempts,
					Task: &job.TaskStep{
						Name:    t.Name,
						Run:     t.Run,
						Inputs:  t.Inputs,
						Outputs: t.Outputs,
					},
					OnSuccess: parseHooks(innerBlock, pairEctx, "on_success"),
					OnFailure: parseHooks(innerBlock, pairEctx, "on_failure"),
					OnCancel:  parseHooks(innerBlock, pairEctx, "on_cancel"),
					Ensure:    parseHooks(innerBlock, pairEctx, "ensure"),
				})
			case "put":
				if putIdx >= len(hj.Put) {
					continue
				}
				p := hj.Put[putIdx]
				putIdx++
				var timeout time.Duration
				if p.Timeout != "" {
					var err error
					timeout, err = time.ParseDuration(p.Timeout)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("invalid timeout %q on put step %q: %w", p.Timeout, p.Name, err)
					}
				}
				if p.Attempts < 0 {
					return nil, nil, nil, fmt.Errorf("invalid attempts %d on put step %q: must be >= 0", p.Attempts, p.Name)
				}
				putParams := make(map[string]string)
				for name, attr := range innerBlock.Body.Attributes {
					if name == "timeout" || name == "attempts" {
						continue
					}
					val, vdiags := attr.Expr.Value(pairEctx)
					if vdiags.HasErrors() {
						continue
					}
					putParams[name] = val.AsString()
				}

				plan = append(plan, job.PlanStep{
					Type:     job.StepTypePut,
					Timeout:  timeout,
					Attempts: p.Attempts,
					Put: &job.PutStep{
						Type:   p.Type,
						Name:   p.Name,
						Params: putParams,
					},
					OnSuccess: parseHooks(innerBlock, pairEctx, "on_success"),
					OnFailure: parseHooks(innerBlock, pairEctx, "on_failure"),
					OnCancel:  parseHooks(innerBlock, pairEctx, "on_cancel"),
					Ensure:    parseHooks(innerBlock, pairEctx, "ensure"),
				})
			case "notify":
				if notifyIdx >= len(hj.Notify) {
					continue
				}
				n := hj.Notify[notifyIdx]
				notifyIdx++
				notifyParams := make(map[string]string)
				for name, attr := range innerBlock.Body.Attributes {
					if name == "message" {
						continue
					}
					val, vdiags := attr.Expr.Value(pairEctx)
					if vdiags.HasErrors() {
						continue
					}
					notifyParams[name] = val.AsString()
				}

				plan = append(plan, job.PlanStep{
					Type: job.StepTypeNotify,
					Notify: &job.NotifyStep{
						Type:    n.Type,
						Name:    n.Name,
						Params:  notifyParams,
						Message: n.Message,
					},
					OnSuccess: parseHooks(innerBlock, pairEctx, "on_success"),
					OnFailure: parseHooks(innerBlock, pairEctx, "on_failure"),
					OnCancel:  parseHooks(innerBlock, pairEctx, "on_cancel"),
					Ensure:    parseHooks(innerBlock, pairEctx, "ensure"),
				})
			}
		}

		result[hj.Name] = plan

		jh := jobHooks{
			OnSuccess: parseHooks(block, pairEctx, "on_success"),
			OnFailure: parseHooks(block, pairEctx, "on_failure"),
			OnCancel:  parseHooks(block, pairEctx, "on_cancel"),
			Ensure:    parseHooks(block, pairEctx, "ensure"),
		}
		jhMap[hj.Name] = jh
	}

	return result, jhMap, services, nil
}
