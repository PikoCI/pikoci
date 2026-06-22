package http

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci"
	"github.com/pikoci/pikoci/pikoci/transport/http/assets"
)

// LocalEditorHandler creates an HTTP handler for the local pipeline editor.
// It serves the editor UI pre-loaded with the HCL file at filePath,
// provides graph preview via the existing createPipelineImage handler,
// and writes changes back to disk on save.
func LocalEditorHandler(s pikoci.Service, filePath string, l *slog.Logger) http.Handler {
	r := mux.NewRouter()

	// API: load initial config
	r.Methods(http.MethodGet).Path("/local/config").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := os.ReadFile(filePath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]string{
			"raw":  base64.StdEncoding.EncodeToString(data),
			"name": filepath.Base(filePath),
		})
	})

	// API: save file
	r.Methods(http.MethodPost).Path("/local/save").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Body = http.MaxBytesReader(w, req.Body, 10<<20) // 10MB limit
		var body struct {
			Config []byte `json:"config"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := os.WriteFile(filePath, body.Config, 0644); err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Graph preview: reuse existing handler
	r.Methods(http.MethodPost).Path("/teams/{team_canonical}/pipelines/image{ext}").Handler(createPipelineImage(s))

	// Static assets
	r.PathPrefix("/css/").Handler(http.FileServer(http.FS(assets.Assets)))
	r.PathPrefix("/js/").Handler(http.FileServer(http.FS(assets.Assets)))
	r.PathPrefix("/images/").Handler(http.FileServer(http.FS(assets.Assets)))
	r.PathPrefix("/fonts/").Handler(http.FileServer(http.FS(assets.Assets)))

	// Root: serve local editor HTML
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(localEditorHTML))
	})

	return r
}

const localEditorHTML = `<!DOCTYPE html>
<html lang="en">
  <head>
    <title>PikoCI - Local Editor</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
    <link href="/css/bootstrap.min.css" rel="stylesheet" />
    <link href="/css/pikoci.css" rel="stylesheet" />
    <link href="/css/bootstrap-icons.min.css" rel="stylesheet" />
    <link href="/images/favicon.svg" rel="icon" type="image/svg+xml"/>
  </head>
  <body>
    <script>
      if (localStorage.getItem('piko-theme') === 'dark') {
        document.documentElement.setAttribute('data-theme', 'dark');
      }
    </script>

    <div id="app"></div>

    <script type="importmap">
    {
      "imports": {
        "preact": "/js/vendor/preact.module.js",
        "preact/": "/js/vendor/preact/",
        "preact/hooks": "/js/vendor/preact/hooks.module.js",
        "htm": "/js/vendor/htm.module.js",
        "htm/preact": "/js/vendor/htm-preact.module.js",
        "preact-router": "/js/vendor/preact-router.module.js",
        "preact-router/match": "/js/vendor/preact-router-match.module.js",
        "@preact/signals": "/js/vendor/signals.module.js",
        "@preact/signals-core": "/js/vendor/signals-core.module.js"
      }
    }
    </script>

    <script type="text/javascript" src="/js/bootstrap.bundle.min.js"></script>
    <script type="text/javascript" src="/js/viz-global.js"></script>
    <script type="text/javascript" src="/js/codemirror-hcl.min.js"></script>
    <script type="module" src="/js/app/local-app.js"></script>
  </body>
</html>`
