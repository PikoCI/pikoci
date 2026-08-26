package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesLoaded(t *testing.T) {
	if _, ok := Templates["views/layouts/index.tmpl"]; !ok {
		keys := make([]string, 0, len(Templates))
		for k := range Templates {
			keys = append(keys, k)
		}
		t.Fatalf("expected Templates to contain %q, got keys: %v", "views/layouts/index.tmpl", keys)
	}
}

// TestNoFilepathImport guards against reintroducing path/filepath for embed.FS
// lookups. filepath.Join and path.Join produce identical output on the
// Linux/macOS CI runner, so a runtime check on Templates' keys can never
// catch this regression here — only a real Windows run would panic. This
// static check on the source fails on every OS instead.
func TestNoFilepathImport(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to list package files: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read %s: %v", f, err)
		}
		if strings.Contains(string(src), `"path/filepath"`) {
			t.Errorf("%s must not import path/filepath: embed.FS lookups require forward slashes on every OS, use the path package instead", f)
		}
	}
}
