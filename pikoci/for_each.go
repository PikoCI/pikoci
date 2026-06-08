package pikoci

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gosimple/slug"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// forEachInstance represents a single expanded instance of a for_each or matrix job.
type forEachInstance struct {
	key      string    // original key (e.g., "1.21", "old", "1-21-linux")
	slugKey  string    // slugified key for canonical name (e.g., "1-21", "old", "1-21-linux")
	eachVal  cty.Value // the cty value for "each" variable
}

// forEachExpansion holds expansion data for a single for_each/matrix job.
type forEachExpansion struct {
	baseName   string
	instances  []forEachInstance
	blockRange hcl.Range // byte range of the job block in the source
}

// slugifyKey converts a for_each key into a valid slug for use in canonical names.
func slugifyKey(key string) string {
	return slug.Make(key)
}

// detectForEachExpansions pre-scans the HCL AST to find job blocks with for_each
// or matrix, evaluates their expressions, and returns expansion data.
func detectForEachExpansions(rpp []byte, ectx *hcl.EvalContext) ([]forEachExpansion, error) {
	file, diags := hclsyntax.ParseConfig(rpp, "pipeline.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse pipeline HCL: %s", diags.Error())
	}

	body := file.Body.(*hclsyntax.Body)
	var expansions []forEachExpansion

	for _, block := range body.Blocks {
		if block.Type != "job" || len(block.Labels) == 0 {
			continue
		}
		jobName := block.Labels[0]

		forEachAttr, hasForEach := block.Body.Attributes["for_each"]
		var matrixBlock *hclsyntax.Block
		for _, inner := range block.Body.Blocks {
			if inner.Type == "matrix" {
				matrixBlock = inner
				break
			}
		}

		if hasForEach && matrixBlock != nil {
			return nil, fmt.Errorf("job %q: for_each and matrix are mutually exclusive", jobName)
		}

		if !hasForEach && matrixBlock == nil {
			continue
		}

		var instances []forEachInstance
		var err error

		if hasForEach {
			instances, err = expandForEach(forEachAttr, ectx)
			if err != nil {
				return nil, fmt.Errorf("job %q: %w", jobName, err)
			}
		} else {
			instances, err = expandMatrix(matrixBlock, ectx)
			if err != nil {
				return nil, fmt.Errorf("job %q: %w", jobName, err)
			}
		}

		if len(instances) == 0 {
			return nil, fmt.Errorf("job %q: for_each/matrix produced no instances", jobName)
		}

		// Validate no duplicate slugified keys
		slugs := make(map[string]string) // slug -> original key
		for _, inst := range instances {
			if existing, ok := slugs[inst.slugKey]; ok {
				return nil, fmt.Errorf("job %q: keys %q and %q produce the same slug %q", jobName, existing, inst.key, inst.slugKey)
			}
			slugs[inst.slugKey] = inst.key
		}

		expansions = append(expansions, forEachExpansion{
			baseName:   jobName,
			instances:  instances,
			blockRange: block.Range(),
		})
	}

	return expansions, nil
}

// expandForEach evaluates a for_each attribute and produces instances.
func expandForEach(attr *hclsyntax.Attribute, ectx *hcl.EvalContext) ([]forEachInstance, error) {
	val, diags := attr.Expr.Value(ectx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to evaluate for_each: %s", diags.Error())
	}

	if !val.IsKnown() {
		return nil, fmt.Errorf("for_each value is not known")
	}

	if val.IsNull() {
		return nil, fmt.Errorf("for_each value is null")
	}

	ty := val.Type()

	switch {
	case ty.IsSetType():
		if ty.ElementType() != cty.String {
			return nil, fmt.Errorf("for_each set must contain strings, got set of %s", ty.ElementType().FriendlyName())
		}
		return expandForEachSet(val)
	case ty.IsObjectType(), ty.IsMapType():
		return expandForEachMap(val)
	default:
		return nil, fmt.Errorf("for_each must be a set (use toset()) or a map, got %s", ty.FriendlyName())
	}
}

// expandForEachSet expands a set of strings into instances.
func expandForEachSet(val cty.Value) ([]forEachInstance, error) {
	var instances []forEachInstance
	for it := val.ElementIterator(); it.Next(); {
		_, elem := it.Element()
		key := elem.AsString()
		sk := slugifyKey(key)
		if sk == "" {
			return nil, fmt.Errorf("key %q produces an empty slug", key)
		}
		instances = append(instances, forEachInstance{
			key:     key,
			slugKey: sk,
			eachVal: cty.ObjectVal(map[string]cty.Value{
				"key":   cty.StringVal(key),
				"value": cty.StringVal(key),
			}),
		})
	}
	// Sort by key for deterministic ordering
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].key < instances[j].key
	})
	return instances, nil
}

// expandForEachMap expands a map/object into instances.
func expandForEachMap(val cty.Value) ([]forEachInstance, error) {
	var instances []forEachInstance
	for it := val.ElementIterator(); it.Next(); {
		k, v := it.Element()
		key := k.AsString()
		sk := slugifyKey(key)
		if sk == "" {
			return nil, fmt.Errorf("key %q produces an empty slug", key)
		}
		instances = append(instances, forEachInstance{
			key:     key,
			slugKey: sk,
			eachVal: cty.ObjectVal(map[string]cty.Value{
				"key":   cty.StringVal(key),
				"value": v,
			}),
		})
	}
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].key < instances[j].key
	})
	return instances, nil
}

// expandMatrix computes the cartesian product of matrix axes and produces instances.
func expandMatrix(block *hclsyntax.Block, ectx *hcl.EvalContext) ([]forEachInstance, error) {
	if len(block.Body.Attributes) == 0 {
		return nil, fmt.Errorf("matrix block has no axes")
	}

	// Collect axes in sorted order for deterministic expansion
	type axis struct {
		name   string
		values []string
	}
	var axes []axis
	for name, attr := range block.Body.Attributes {
		val, diags := attr.Expr.Value(ectx)
		if diags.HasErrors() {
			return nil, fmt.Errorf("failed to evaluate matrix axis %q: %s", name, diags.Error())
		}
		if !val.Type().IsListType() && !val.Type().IsTupleType() && !val.Type().IsSetType() {
			return nil, fmt.Errorf("matrix axis %q must be a list, got %s", name, val.Type().FriendlyName())
		}
		var values []string
		for it := val.ElementIterator(); it.Next(); {
			_, elem := it.Element()
			if elem.Type() != cty.String {
				return nil, fmt.Errorf("matrix axis %q must contain strings", name)
			}
			values = append(values, elem.AsString())
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("matrix axis %q is empty", name)
		}
		axes = append(axes, axis{name: name, values: values})
	}
	sort.Slice(axes, func(i, j int) bool {
		return axes[i].name < axes[j].name
	})

	// Compute cartesian product
	combos := []map[string]string{{}}
	for _, ax := range axes {
		var next []map[string]string
		for _, combo := range combos {
			for _, v := range ax.values {
				nc := make(map[string]string, len(combo)+1)
				for k, cv := range combo {
					nc[k] = cv
				}
				nc[ax.name] = v
				next = append(next, nc)
			}
		}
		combos = next
	}

	var instances []forEachInstance
	for _, combo := range combos {
		// Build composite key from axis values in sorted axis order
		var keyParts []string
		valueFields := make(map[string]cty.Value)
		for _, ax := range axes {
			v := combo[ax.name]
			keyParts = append(keyParts, slugifyKey(v))
			valueFields[ax.name] = cty.StringVal(v)
		}
		compositeKey := strings.Join(keyParts, "-")

		instances = append(instances, forEachInstance{
			key:     compositeKey,
			slugKey: compositeKey,
			eachVal: cty.ObjectVal(map[string]cty.Value{
				"key":   cty.StringVal(compositeKey),
				"value": cty.ObjectVal(valueFields),
			}),
		})
	}

	return instances, nil
}

// stripForEachJobBlocks removes for_each/matrix job blocks from the raw HCL bytes
// and returns the modified bytes along with the extracted block bytes per job name.
func stripForEachJobBlocks(rpp []byte, expansions []forEachExpansion) ([]byte, map[string][]byte) {
	if len(expansions) == 0 {
		return rpp, nil
	}

	// Sort expansions by block range start in reverse order so we can
	// remove from end to start without invalidating earlier offsets.
	sorted := make([]forEachExpansion, len(expansions))
	copy(sorted, expansions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].blockRange.Start.Byte > sorted[j].blockRange.Start.Byte
	})

	blockBytes := make(map[string][]byte)
	result := make([]byte, len(rpp))
	copy(result, rpp)

	for _, exp := range sorted {
		start := exp.blockRange.Start.Byte
		end := exp.blockRange.End.Byte
		blockBytes[exp.baseName] = rpp[start:end]
		result = append(result[:start], result[end:]...)
	}

	return result, blockBytes
}

// removeForEachAndMatrixFromBlock removes the for_each attribute and matrix block
// from job block bytes, leaving only the regular job content.
func removeForEachAndMatrixFromBlock(blockBytes []byte) ([]byte, error) {
	file, diags := hclsyntax.ParseConfig(blockBytes, "job.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("failed to parse job block: %s", diags.Error())
	}
	body := file.Body.(*hclsyntax.Body)
	if len(body.Blocks) == 0 {
		return nil, fmt.Errorf("no job block found")
	}
	jobBlock := body.Blocks[0]

	// Collect ranges to remove (for_each attr and matrix blocks)
	type rangeToRemove struct {
		start, end int
	}
	var removals []rangeToRemove

	if attr, ok := jobBlock.Body.Attributes["for_each"]; ok {
		removals = append(removals, rangeToRemove{
			start: attr.SrcRange.Start.Byte,
			end:   attr.SrcRange.End.Byte,
		})
	}
	for _, inner := range jobBlock.Body.Blocks {
		if inner.Type == "matrix" {
			r := inner.Range()
			removals = append(removals, rangeToRemove{
				start: r.Start.Byte,
				end:   r.End.Byte,
			})
		}
	}

	if len(removals) == 0 {
		return blockBytes, nil
	}

	// Sort removals in reverse order
	sort.Slice(removals, func(i, j int) bool {
		return removals[i].start > removals[j].start
	})

	result := make([]byte, len(blockBytes))
	copy(result, blockBytes)
	for _, r := range removals {
		result = append(result[:r.start], result[r.end:]...)
	}

	return result, nil
}

// makeInstanceEctx creates a copy of the eval context with the "each" variable
// set to the given instance value.
func makeInstanceEctx(baseEctx *hcl.EvalContext, eachVal cty.Value) *hcl.EvalContext {
	vars := make(map[string]cty.Value, len(baseEctx.Variables)+1)
	for k, v := range baseEctx.Variables {
		vars[k] = v
	}
	vars["each"] = eachVal
	return &hcl.EvalContext{
		Variables: vars,
		Functions: baseEctx.Functions,
	}
}
