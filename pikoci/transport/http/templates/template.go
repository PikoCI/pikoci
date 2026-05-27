// Package templates provides embedded HTML templates for the PikoCI web interface.
// Templates are loaded from the embedded views directory on package initialization
// and cached in a map for fast lookup during request handling.
package templates

import (
	"embed"
	"io/fs"
	"path/filepath"
	"text/template"
)

const (
	// viewsDir is the root directory for template files.
	viewsDir = "views"
	// extension is the glob pattern for template file matching.
	extension = "/*.tmpl"
)

var (
	// layoutsDir is the directory containing layout template files.
	layoutsDir = filepath.Join(viewsDir, "layouts")

	//go:embed views/layouts/*
	files embed.FS

	// Templates is the cache of all parsed templates, keyed by their relative
	// path within the embedded filesystem (e.g., "views/layouts/index.tmpl").
	Templates map[string]*template.Template
)

func init() {
	if Templates == nil {
		Templates = make(map[string]*template.Template)
	}

	loadTemplates(viewsDir)
}

func loadTemplates(path string) error {
	tmplFiles, err := fs.ReadDir(files, path)
	if err != nil {
		panic(err)
	}

	for _, tmpl := range tmplFiles {
		if tmpl.IsDir() {
			loadTemplates(filepath.Join(path, tmpl.Name()))
			continue
		}

		newpath := filepath.Join(path, tmpl.Name())

		if _, ok := Templates[newpath]; ok {
			continue
		}

		pt := template.New(tmpl.Name())

		pt, err := pt.ParseFS(files, newpath, filepath.Join(layoutsDir, extension))
		if err != nil {
			panic(err)
		}

		Templates[newpath] = pt
	}

	return nil
}
