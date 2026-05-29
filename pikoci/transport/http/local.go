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
    <div id="app">
    </div>

    <script type="text/javascript" src="/js/jquery.min.js"></script>
    <script type="text/javascript" src="/js/underscorejs.min.js"></script>
    <script type="text/javascript" src="/js/backbonejs.min.js"></script>
    <script type="text/javascript" src="/js/bootstrap.bundle.min.js"></script>
    <script type="text/javascript" src="/js/viz-global.js"></script>
    <script type="text/javascript" src="/js/codemirror-hcl.min.js"></script>

    <script type="text/template" id="main-view">
      <main class="container">
        <div id="notice"></div>
        <div id="main"></div>
      </main>
    </script>

    <script type="text/template" id="notice-view">
      <div>
        <% if (error) { %>
          <div class="alert alert-danger" role="alert">
            <%- error %>
          </div>
        <% } %>
        <% if (success) { %>
          <div class="alert alert-success" role="alert">
            <%- success %>
          </div>
        <% } %>
      </div>
    </script>

    <script type="text/template" id="pipeline-graph-view">
      <div id="pipeline-graph"></div>
    </script>

    <script type="text/template" id="pipelines-new-view">
      <div class="piko-page-header mb-3">
        <h1 class="h4 fw-bold mb-0">Local Editor</h1>
      </div>
      <form>
        <div class="piko-settings-row mb-3" style="display:none;">
          <div class="piko-field">
            <label class="form-label">Name</label>
            <input type="text" class="form-control" id="name" value="<%- name %>" style="width:280px">
          </div>
          <div class="piko-checkbox-inline">
            <input type="checkbox" class="form-check-input" id="public">
            <label class="form-check-label" for="public">Public</label>
          </div>
        </div>
        <div class="piko-editor-card" id="editor-card">
          <div class="piko-editor-toolbar">
            <div class="piko-tab active" id="tab-hcl">pipeline.hcl</div>
            <div class="piko-tab" id="tab-vars">vars.json</div>
            <div class="piko-toolbar-spacer"></div>
            <div class="piko-docs-dropdown" id="docs-dropdown">
              <button type="button" class="piko-tbtn" id="docs-btn" title="Pipeline documentation">
                <i class="bi bi-book"></i> <span class="piko-tbtn-label">Docs</span> <i class="bi bi-chevron-down" style="font-size:0.6rem;margin-left:2px"></i>
              </button>
              <div class="piko-docs-menu" id="docs-menu">
                <a href="https://docs.pikoci.com/Pipeline" target="_blank" rel="noopener"><i class="bi bi-file-text"></i> Pipeline overview</a>
                <div class="piko-docs-divider"></div>
                <div class="piko-docs-label">Blocks</div>
                <a href="https://docs.pikoci.com/Pipeline#job" target="_blank" rel="noopener"><span class="piko-block-icon jb">J</span> job</a>
                <a href="https://docs.pikoci.com/Pipeline#resource" target="_blank" rel="noopener"><span class="piko-block-icon rs">R</span> resource</a>
                <a href="https://docs.pikoci.com/Pipeline#resource_type" target="_blank" rel="noopener"><span class="piko-block-icon rt">R</span> resource_type</a>
                <a href="https://docs.pikoci.com/Pipeline#runner_type" target="_blank" rel="noopener"><span class="piko-block-icon rn">R</span> runner_type</a>
                <a href="https://docs.pikoci.com/Pipeline#secret_type" target="_blank" rel="noopener"><span class="piko-block-icon st">S</span> secret_type</a>
                <a href="https://docs.pikoci.com/Pipeline#service_type" target="_blank" rel="noopener"><span class="piko-block-icon sv">S</span> service_type</a>
                <a href="https://docs.pikoci.com/Pipeline#variable" target="_blank" rel="noopener"><span class="piko-block-icon vr">V</span> variable</a>
              </div>
            </div>
            <div class="piko-toolbar-sep"></div>
            <button type="button" class="piko-tbtn active" id="blocks-btn" title="Toggle blocks panel">
              <i class="bi bi-list-nested"></i> <span class="piko-tbtn-label">Blocks</span>
              <span class="piko-error-badge"></span>
            </button>
            <div class="piko-toolbar-sep"></div>
            <button type="button" class="piko-tbtn" id="graph-btn" title="Toggle graph preview">
              <i class="bi bi-diagram-3"></i>
            </button>
            <div class="piko-toolbar-sep"></div>
            <button type="button" class="piko-tbtn" id="fullscreen-btn" title="Fullscreen (Esc to exit)">
              <i class="bi bi-arrows-fullscreen"></i>
            </button>
          </div>
          <div class="piko-editor-body">
            <div class="piko-blocks-panel" id="blocks-panel"></div>
            <div class="piko-code-area" id="code-area">
              <div id="pipeline-editor"></div>
              <textarea id="pipeline" style="display:none"><%- raw %></textarea>
            </div>
            <div class="piko-vars-area" id="vars-area">
              <textarea type="text" rows="10" class="form-control" id="vars" placeholder='{"key": "value"}'></textarea>
              <div class="piko-vars-hint">JSON object passed as variables to the pipeline definition.</div>
            </div>
          </div>
          <div class="piko-graph-bottom" id="graph-bottom-panel">
            <div class="piko-graph-bottom-header">
              <span><i class="bi bi-diagram-3"></i> Graph Preview</span>
              <button type="button" id="graph-bottom-close" title="Close"><i class="bi bi-x-lg"></i></button>
            </div>
            <div class="piko-graph-bottom-body" id="graph-fullscreen"></div>
          </div>
        </div>
        <div class="piko-graph-strip" id="graph-strip">
          <div class="piko-graph-strip-header" id="graph-strip-header">
            <span><i class="bi bi-diagram-3"></i> Graph Preview</span>
            <i class="bi bi-chevron-right piko-graph-chev"></i>
          </div>
          <div class="piko-graph-strip-body" id="graph">
          </div>
        </div>
        <button type="submit" class="btn btn-primary mt-2" id="create">Save</button>
      </form>
    </script>

    <script type="text/javascript">
      'use strict';
      (function() {
        var saved = localStorage.getItem('piko-theme');
        if (saved === 'dark') {
          document.documentElement.setAttribute('data-theme', 'dark');
        }
      })();
    </script>

    <script type="module" src="/js/app/local-init.js"></script>

  </body>
</html>`
