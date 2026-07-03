package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/pikoci/pikoci/pikoci/pipeline"
	"github.com/pikoci/pikoci/pikoci/sectype"
)

func TestBuildSecretDependencyGraph(t *testing.T) {
	t.Run("no dependencies", func(t *testing.T) {
		secretVars := map[string]pipeline.VariableSecret{
			"a": {Type: "env", Key: "A"},
			"b": {Type: "env", Key: "B"},
		}
		pp := &pipeline.Pipeline{}

		g := buildSecretDependencyGraph(secretVars, pp)

		// No variable depends on any other
		for _, deps := range g.deps {
			assert.Empty(t, deps)
		}
	})

	t.Run("linear chain A depends on B", func(t *testing.T) {
		secretVars := map[string]pipeline.VariableSecret{
			"b": {Type: "env", Key: "B_KEY"},
			"a": {Type: "file", Path: "__pikoci_secret:env::B_KEY__", Key: "content"},
		}
		pp := &pipeline.Pipeline{}

		g := buildSecretDependencyGraph(secretVars, pp)

		assert.True(t, g.deps["a"]["b"])
		assert.Empty(t, g.deps["b"])
	})

	t.Run("diamond dependency", func(t *testing.T) {
		secretVars := map[string]pipeline.VariableSecret{
			"c": {Type: "env", Key: "C_KEY"},
			"b": {Type: "env", Key: "B_KEY"},
			"a": {
				Type: "vault",
				Path: "__pikoci_secret:env::B_KEY__",
				Key:  "__pikoci_secret:env::C_KEY__",
			},
		}
		pp := &pipeline.Pipeline{}

		g := buildSecretDependencyGraph(secretVars, pp)

		assert.True(t, g.deps["a"]["b"])
		assert.True(t, g.deps["a"]["c"])
		assert.Empty(t, g.deps["b"])
		assert.Empty(t, g.deps["c"])
	})

	t.Run("secret_type config dependency", func(t *testing.T) {
		secretVars := map[string]pipeline.VariableSecret{
			"vault_addr": {Type: "env", Key: "VAULT_ADDR"},
			"db_pass":    {Type: "vault", Path: "secret/data/db", Key: "password"},
		}
		pp := &pipeline.Pipeline{
			SecretTypes: []sectype.SecretType{
				{
					Name:   "vault",
					Config: map[string]string{"address": "__pikoci_secret:env::VAULT_ADDR__"},
				},
			},
		}

		g := buildSecretDependencyGraph(secretVars, pp)

		// db_pass depends on vault_addr via secret_type config
		assert.True(t, g.deps["db_pass"]["vault_addr"])
		assert.Empty(t, g.deps["vault_addr"])
	})
}

func TestSecretDepGraph_DetectCycle(t *testing.T) {
	t.Run("no cycle", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true},
			deps: map[string]map[string]bool{
				"a": {"b": true},
				"b": {},
			},
		}
		assert.Nil(t, g.detectCycle())
	})

	t.Run("circular two vars", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true},
			deps: map[string]map[string]bool{
				"a": {"b": true},
				"b": {"a": true},
			},
		}
		cycle := g.detectCycle()
		require.NotNil(t, cycle)
		// Deterministic: sorted iteration starts at "a", follows a -> b -> a
		assert.Equal(t, []string{"a", "b", "a"}, cycle)
	})

	t.Run("self-referential", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true},
			deps: map[string]map[string]bool{
				"a": {"a": true},
			},
		}
		cycle := g.detectCycle()
		require.NotNil(t, cycle)
		assert.Equal(t, []string{"a", "a"}, cycle)
	})

	t.Run("circular three vars", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true, "c": true},
			deps: map[string]map[string]bool{
				"a": {"b": true},
				"b": {"c": true},
				"c": {"a": true},
			},
		}
		cycle := g.detectCycle()
		require.NotNil(t, cycle)
		assert.Equal(t, []string{"a", "b", "c", "a"}, cycle)
	})
}

func TestSecretDepGraph_TopologicalSort(t *testing.T) {
	t.Run("no dependencies single layer", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true, "c": true},
			deps: map[string]map[string]bool{
				"a": {},
				"b": {},
				"c": {},
			},
		}
		layers, err := g.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 1)
		assert.Equal(t, []string{"a", "b", "c"}, layers[0])
	})

	t.Run("linear chain", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true, "c": true},
			deps: map[string]map[string]bool{
				"a": {"b": true},
				"b": {"c": true},
				"c": {},
			},
		}
		layers, err := g.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 3)
		assert.Equal(t, []string{"c"}, layers[0])
		assert.Equal(t, []string{"b"}, layers[1])
		assert.Equal(t, []string{"a"}, layers[2])
	})

	t.Run("diamond", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true, "c": true},
			deps: map[string]map[string]bool{
				"a": {"b": true, "c": true},
				"b": {},
				"c": {},
			},
		}
		layers, err := g.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 2)
		assert.Equal(t, []string{"b", "c"}, layers[0])
		assert.Equal(t, []string{"a"}, layers[1])
	})

	t.Run("mixed deps and no deps", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true, "c": true, "d": true},
			deps: map[string]map[string]bool{
				"a": {"b": true},
				"b": {},
				"c": {},
				"d": {},
			},
		}
		layers, err := g.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 2)
		assert.Equal(t, []string{"b", "c", "d"}, layers[0])
		assert.Equal(t, []string{"a"}, layers[1])
	})

	t.Run("circular returns error", func(t *testing.T) {
		g := &secretDepGraph{
			vars: map[string]bool{"a": true, "b": true},
			deps: map[string]map[string]bool{
				"a": {"b": true},
				"b": {"a": true},
			},
		}
		_, err := g.topologicalSort()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular secret dependency")
	})

	t.Run("max depth exceeded", func(t *testing.T) {
		// Build a chain of 11 levels
		vars := make(map[string]bool)
		deps := make(map[string]map[string]bool)
		for i := 0; i <= 10; i++ {
			name := string(rune('a' + i))
			vars[name] = true
			deps[name] = make(map[string]bool)
			if i > 0 {
				prev := string(rune('a' + i - 1))
				deps[name][prev] = true
			}
		}
		g := &secretDepGraph{vars: vars, deps: deps}
		_, err := g.topologicalSort()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret dependency chain too deep")
	})
}
