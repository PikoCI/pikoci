package worker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pikoci/pikoci/pikoci/pipeline"
)

const maxSecretChainDepth = 10

// secretDepGraph represents a dependency graph between secret-backed variables.
type secretDepGraph struct {
	// deps maps variable name → set of variable names it depends on
	deps map[string]map[string]bool
	// all variable names that are part of the graph
	vars map[string]bool
}

// buildSecretDependencyGraph builds a dependency graph for secret-backed variables.
// It scans each variable's path, key, and associated secret_type config for
// placeholders that reference other secret variables.
func buildSecretDependencyGraph(secretVars map[string]pipeline.VariableSecret, pp *pipeline.Pipeline) *secretDepGraph {
	// Build reverse index: placeholder → variable name
	placeholderToVar := make(map[string]string)
	for varName, sv := range secretVars {
		placeholder := fmt.Sprintf("__pikoci_secret:%s:%s:%s__", sv.Type, sv.Path, sv.Key)
		placeholderToVar[placeholder] = varName
	}

	g := &secretDepGraph{
		deps: make(map[string]map[string]bool),
		vars: make(map[string]bool),
	}

	for varName, sv := range secretVars {
		g.vars[varName] = true
		g.deps[varName] = make(map[string]bool)

		// Scan path and key for placeholders
		scanForDeps(g, varName, sv.Path, placeholderToVar)
		scanForDeps(g, varName, sv.Key, placeholderToVar)

		// Scan associated secret_type config
		st, ok := pp.SecretType(sv.Type)
		if ok {
			for _, configVal := range st.Config {
				scanForDeps(g, varName, configVal, placeholderToVar)
			}
		}
	}

	return g
}

// scanForDeps finds all placeholders in s and adds dependency edges from varName
// to the variable that owns each placeholder.
func scanForDeps(g *secretDepGraph, varName, s string, placeholderToVar map[string]string) {
	for placeholder, depVar := range placeholderToVar {
		if strings.Contains(s, placeholder) {
			g.deps[varName][depVar] = true
		}
	}
}

// sortedKeys returns sorted keys from a map for deterministic iteration.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// detectCycle returns a cycle path if one exists, or nil.
// Iteration order is deterministic (sorted) for reproducible error messages.
func (g *secretDepGraph) detectCycle() []string {
	visited := make(map[string]int) // 0=unvisited, 1=in-stack, 2=done
	var path []string

	var dfs func(node string) []string
	dfs = func(node string) []string {
		visited[node] = 1
		path = append(path, node)
		for _, dep := range sortedKeys(g.deps[node]) {
			if visited[dep] == 1 {
				// Found cycle — extract cycle from path
				cycleStart := -1
				for i, n := range path {
					if n == dep {
						cycleStart = i
						break
					}
				}
				cycle := make([]string, len(path[cycleStart:]))
				copy(cycle, path[cycleStart:])
				cycle = append(cycle, dep)
				return cycle
			}
			if visited[dep] == 0 {
				if cycle := dfs(dep); cycle != nil {
					return cycle
				}
			}
		}
		path = path[:len(path)-1]
		visited[node] = 2
		return nil
	}

	for _, v := range sortedKeys(g.vars) {
		if visited[v] == 0 {
			if cycle := dfs(v); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// topologicalSort returns variables grouped into layers. Layer 0 has no
// dependencies, layer 1 depends only on layer 0, etc. Returns an error
// if a cycle exists or chain depth exceeds maxSecretChainDepth.
func (g *secretDepGraph) topologicalSort() ([][]string, error) {
	// Check for cycles
	if cycle := g.detectCycle(); cycle != nil {
		return nil, fmt.Errorf("circular secret dependency: %s", strings.Join(cycle, " -> "))
	}

	remaining := make(map[string]map[string]bool)
	for v, deps := range g.deps {
		remaining[v] = make(map[string]bool)
		for d := range deps {
			remaining[v][d] = true
		}
	}

	var layers [][]string
	resolved := make(map[string]bool)

	for len(remaining) > 0 {
		if len(layers) >= maxSecretChainDepth {
			return nil, fmt.Errorf("secret dependency chain too deep (max %d levels)", maxSecretChainDepth)
		}

		// Find all vars with no unresolved deps
		var layer []string
		for v, deps := range remaining {
			allResolved := true
			for d := range deps {
				if !resolved[d] {
					allResolved = false
					break
				}
			}
			if allResolved {
				layer = append(layer, v)
			}
		}

		if len(layer) == 0 {
			// Should not happen if cycle detection works, but guard anyway
			return nil, fmt.Errorf("internal error: no progress in topological sort")
		}

		sort.Strings(layer)
		layers = append(layers, layer)
		for _, v := range layer {
			resolved[v] = true
			delete(remaining, v)
		}
	}

	return layers, nil
}
