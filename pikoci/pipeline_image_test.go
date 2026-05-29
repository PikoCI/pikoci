package pikoci

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDOT(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("graphviz dot binary not available")
	}

	ctx := context.Background()
	dot := []byte(`digraph G { A -> B; }`)

	t.Run("svg", func(t *testing.T) {
		out, err := renderDOT(ctx, dot, "svg")
		require.NoError(t, err)
		assert.Contains(t, string(out), "<svg")
		assert.Contains(t, string(out), "</svg>")
	})

	t.Run("png", func(t *testing.T) {
		out, err := renderDOT(ctx, dot, "png")
		require.NoError(t, err)
		// PNG files start with the magic bytes
		assert.True(t, len(out) > 8)
		assert.Equal(t, byte(0x89), out[0])
		assert.Equal(t, byte('P'), out[1])
		assert.Equal(t, byte('N'), out[2])
		assert.Equal(t, byte('G'), out[3])
	})

	t.Run("invalid_dot", func(t *testing.T) {
		_, err := renderDOT(ctx, []byte("not valid dot"), "svg")
		require.Error(t, err)
	})
}

func TestConvertDOTImage(t *testing.T) {
	if _, err := exec.LookPath("dot"); err != nil {
		t.Skip("graphviz dot binary not available")
	}

	ctx := context.Background()
	dot := []byte(`digraph G { A -> B; }`)

	t.Run("dot_passthrough", func(t *testing.T) {
		out, err := convertDOTImage(ctx, dot, "dot")
		require.NoError(t, err)
		assert.Equal(t, dot, out)
	})

	t.Run("svg_with_postprocessing", func(t *testing.T) {
		out, err := convertDOTImage(ctx, dot, "svg")
		require.NoError(t, err)
		result := string(out)
		assert.Contains(t, result, "<svg")
		assert.Contains(t, result, "background: transparent")
		assert.Contains(t, result, "Plus Jakarta Sans")
	})

	t.Run("png", func(t *testing.T) {
		out, err := convertDOTImage(ctx, dot, "png")
		require.NoError(t, err)
		assert.Equal(t, byte(0x89), out[0])
	})
}

func TestPostProcessSVG(t *testing.T) {
	// Minimal SVG mimicking graphviz output
	input := `<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<svg width="200pt" height="100pt" viewBox="0 0 200 100" xmlns="http://www.w3.org/2000/svg">
<g id="graph0" class="graph">
<polygon fill="white" stroke="transparent" points="0,0 0,-100 200,0 200,-100"/>
<g id="node1" class="node">
<polygon fill="#00A83A" stroke="#5F574F" points="10,10 10,50 100,50 100,10 10,10"/>
<text>job1</text>
</g>
<g id="node2" class="node">
<polygon fill="#FF004D" stroke="#5F574F" points="110,10 110,50 190,50 190,10"/>
<text>job2</text>
</g>
</g>
</svg>`

	out, err := postProcessSVG([]byte(input))
	require.NoError(t, err)
	result := string(out)

	// Background should be transparent
	assert.Contains(t, result, `background: transparent`)

	// First polygon (background) should have transparent fill/stroke
	assert.Contains(t, result, `fill="transparent"`)

	// Filled polygons should be converted to rounded rects
	assert.Contains(t, result, `rx="4"`)
	assert.Contains(t, result, `ry="4"`)

	// Font-family should be set on text elements
	assert.Contains(t, result, `Plus Jakarta Sans`)

	// Cursor pointer on node groups
	assert.Contains(t, result, `cursor: pointer`)

	// Original filled polygon tags for jobs should be replaced with rect
	// The 5-point polygon (first node) should become a rect
	if strings.Contains(result, `<polygon fill="#00A83A"`) {
		t.Error("expected filled polygon to be converted to rect")
	}
}

func TestPostProcessSVG_PreservesNonRectPolygons(t *testing.T) {
	// A polygon with 3 points (triangle) should not be converted
	input := `<svg>
<polygon fill="white" stroke="transparent" points="0,0 0,-100 200,0"/>
<polygon fill="#00A83A" stroke="black" points="10,10 50,50 90,10"/>
</svg>`

	out, err := postProcessSVG([]byte(input))
	require.NoError(t, err)
	result := string(out)

	// Triangle polygon should remain (not converted to rect)
	assert.Contains(t, result, `<polygon fill="#00A83A"`)
}

func TestExtractAttr(t *testing.T) {
	assert.Equal(t, "red", extractAttr(`<polygon fill="red" stroke="black"/>`, "fill"))
	assert.Equal(t, "black", extractAttr(`<polygon fill="red" stroke="black"/>`, "stroke"))
	assert.Equal(t, "", extractAttr(`<polygon fill="red"/>`, "stroke"))
}

func TestSetAttr(t *testing.T) {
	t.Run("replace_existing", func(t *testing.T) {
		result := setAttr(`<polygon fill="red"/>`, "fill", "blue")
		assert.Contains(t, result, `fill="blue"`)
		assert.NotContains(t, result, `fill="red"`)
	})

	t.Run("add_new", func(t *testing.T) {
		result := setAttr(`<polygon fill="red"/>`, "stroke", "black")
		assert.Contains(t, result, `stroke="black"`)
		assert.Contains(t, result, `fill="red"`)
	})
}
