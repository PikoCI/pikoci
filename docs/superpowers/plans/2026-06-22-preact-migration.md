# Preact Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace BackboneJS + jQuery + Underscore frontend with Preact + HTM + preact-router + Preact Signals — no build step, no bundler.

**Architecture:** New Preact files are created alongside existing Backbone files. The old code stays untouched until the final swap (Task 15). Each task produces a commit. Selenium integration tests verify the migration at the end.

**Tech Stack:** Preact 10.x, HTM 3.x, preact-router 4.x, @preact/signals 2.x — all vendored as ES modules. Import map in HTML for bare specifier resolution. Go backend serves static files via `embed.FS`.

**Spec:** `docs/superpowers/specs/2026-06-21-backbone-to-preact-migration-design.md`

---

## File Map

### New Files to Create

| File | Responsibility |
|------|---------------|
| `js/vendor/preact.module.js` | Vendored Preact core |
| `js/vendor/preact/hooks.module.js` | Vendored Preact hooks |
| `js/vendor/htm.module.js` | Vendored HTM standalone |
| `js/vendor/htm-preact.module.js` | Vendored HTM bound to Preact |
| `js/vendor/preact-router.module.js` | Vendored preact-router |
| `js/vendor/preact-router-match.module.js` | Vendored preact-router/match |
| `js/vendor/signals.module.js` | Vendored @preact/signals |
| `js/app/app.js` | Entry point — App component with Router (replaces `init.js`) |
| `js/app/local-app.js` | Entry point for local editor mode (replaces `local-init.js`) |
| `js/app/api.js` | Fetch wrappers with auth, token refresh, error handling (replaces `models.js` + `collections.js`) |
| `js/app/state.js` | Preact Signals for session, apiNotice, teams (replaces Backbone singletons) |
| `js/app/utils.js` | Utility functions (replaces `namespace.js`) |
| `js/app/hooks.js` | Custom hooks: `useRequireAuth`, `useLoading` |
| `js/app/toast.js` | Toast notification system with direct function calls |
| `js/app/graph-zoom.js` | PikoGraphZoom class extracted from `views/editor.js` (framework-agnostic) |
| `js/app/components/Layout.js` | Header, Notice/Toast, Breadcrumb |
| `js/app/components/Login.js` | Login form |
| `js/app/components/Teams.js` | Team list, create, show, member management |
| `js/app/components/PipelineList.js` | Pipeline card grid with live status |
| `js/app/components/PipelineShow.js` | Pipeline show: graph/list toggle, resources panel, gear/share panels |
| `js/app/components/PipelineListView.js` | List view: job tree, resource/version selectors, embedded builds |
| `js/app/components/PipelineGraph.js` | Graphviz SVG rendering, SVG post-processing, node click handling |
| `js/app/components/Editor.js` | Pipeline HCL editor with CodeMirror, blocks panel, graph preview |
| `js/app/components/Jobs.js` | Build tabs, build content with step rendering, logs, polling |
| `js/app/components/Resources.js` | Resource versions, pin/unpin, webhook panel |
| `js/app/components/Users.js` | User management + Profile |
| `js/app/components/Workers.js` | Worker list with version mismatch detection |

### Files to Modify

| File | Change |
|------|--------|
| `templates/views/layouts/index.tmpl` | Remove 30 template blocks + old script tags, add import map + new entry point |
| `transport/http/local.go` | Update `localEditorHTML` constant: remove old scripts/templates, add import map + `local-app.js` |

### Files to Delete (Task 15)

Old Backbone files: `jquery.min.js`, `underscorejs.min.js`, `backbonejs.min.js`, `app/models.js`, `app/collections.js`, `app/router.js`, `app/namespace.js`, `app/init.js`, `app/local-init.js`, all 9 files in `app/views/`.

**All paths below are relative to repo root.** JS asset paths are under `pikoci/transport/http/assets/`.

**Note:** The `go:embed` directive in `pikoci/transport/http/assets/assets.go` uses `all:js` which recursively includes everything under `js/` — new files in `js/vendor/` and `js/app/components/` are automatically served. No changes to `assets.go` needed.

**Selenium test baseline:** Already established in a prior commit on the `preact-migration` branch. All 22 new tests pass alongside existing tests. Run `make test-integration` to verify.

**Code style:** Use modern JS (`const`, `let`, arrow functions, spread syntax) matching the spec's examples.

**Rollback:** If tests fail after the HTML swap (Task 16), revert Tasks 16+17 with `git revert HEAD~2` to restore the old entry points.

---

### Task 1: Vendor Preact Libraries

**Files:**
- Create: `pikoci/transport/http/assets/js/vendor/preact.module.js`
- Create: `pikoci/transport/http/assets/js/vendor/preact/hooks.module.js`
- Create: `pikoci/transport/http/assets/js/vendor/htm.module.js`
- Create: `pikoci/transport/http/assets/js/vendor/htm-preact.module.js`
- Create: `pikoci/transport/http/assets/js/vendor/preact-router.module.js`
- Create: `pikoci/transport/http/assets/js/vendor/preact-router-match.module.js`
- Create: `pikoci/transport/http/assets/js/vendor/signals.module.js`

- [ ] **Step 1: Create vendor directory and download libraries**

```bash
cd pikoci/transport/http/assets
mkdir -p js/vendor/preact

# Download from esm.sh with ?bundle&external=preact
# The ?bundle flag creates self-contained files.
# The &external=preact flag keeps bare 'preact' imports so the import map resolves them.
curl -sL -o js/vendor/preact.module.js "https://esm.sh/preact@10.25.4?bundle"
curl -sL -o js/vendor/preact/hooks.module.js "https://esm.sh/preact@10.25.4/hooks?bundle&external=preact"
curl -sL -o js/vendor/htm.module.js "https://esm.sh/htm@3.1.1?bundle"
curl -sL -o js/vendor/htm-preact.module.js "https://esm.sh/htm@3.1.1/preact?bundle&external=preact"
curl -sL -o js/vendor/preact-router.module.js "https://esm.sh/preact-router@4.1.2?bundle&external=preact"
curl -sL -o js/vendor/preact-router-match.module.js "https://esm.sh/preact-router@4.1.2/match?bundle&external=preact"
curl -sL -o js/vendor/signals.module.js "https://esm.sh/@preact/signals@1.3.1?bundle&external=preact"
```

- [ ] **Step 2: Verify downloads are valid JS**

```bash
# Each file should be >1KB and contain JS (not HTML error page)
for f in js/vendor/preact.module.js js/vendor/preact/hooks.module.js js/vendor/htm.module.js js/vendor/htm-preact.module.js js/vendor/preact-router.module.js js/vendor/preact-router-match.module.js js/vendor/signals.module.js; do
  SIZE=$(wc -c < "$f")
  echo "$f: $SIZE bytes"
  if [ "$SIZE" -lt 500 ]; then echo "  ERROR: file too small, likely a redirect or error page"; fi
  head -1 "$f"
done
```

Expected: preact.module.js ~4-15KB, hooks ~2-5KB, htm ~1-3KB, htm-preact ~3-8KB, router ~3-6KB, signals ~2-4KB. All should start with JS code, not `<!DOCTYPE` or `<html>`.

- [ ] **Step 3: Create a minimal test HTML to verify imports work**

Create a temporary test file (not committed) to verify the import map + vendor files work in a browser:

```bash
cat > /tmp/preact-test.html << 'EOF'
<!DOCTYPE html>
<html>
<head>
<script type="importmap">
{"imports":{"preact":"/js/vendor/preact.module.js","preact/":"/js/vendor/preact/","preact/hooks":"/js/vendor/preact/hooks.module.js","htm":"/js/vendor/htm.module.js","htm/preact":"/js/vendor/htm-preact.module.js","preact-router":"/js/vendor/preact-router.module.js","preact-router/match":"/js/vendor/preact-router-match.module.js","@preact/signals":"/js/vendor/signals.module.js"}}
</script>
</head>
<body>
<div id="app"></div>
<script type="module">
import { html, render } from 'htm/preact';
import { useState } from 'preact/hooks';
import { signal } from '@preact/signals';
function App() {
  const [count, setCount] = useState(0);
  return html`<div>
    <h1>Preact works! Count: ${count}</h1>
    <button onClick=${() => setCount(c => c + 1)}>+1</button>
  </div>`;
}
render(html`<${App} />`, document.getElementById('app'));
</script>
</body>
</html>
EOF
echo "Test file created. Open in browser to verify."
```

If the esm.sh `?bundle` approach doesn't resolve imports correctly, fall back to downloading from unpkg and manually verifying, or use `npm pack` to extract the files.

- [ ] **Step 4: Commit**

```bash
git add pikoci/transport/http/assets/js/vendor/
git commit -m "chore: vendor Preact, HTM, preact-router, and @preact/signals ES modules"
```

---

### Task 2: Core Infrastructure — `utils.js`

**Files:**
- Create: `js/app/utils.js`
- Reference: `js/app/namespace.js` (read, do not modify)

- [ ] **Step 1: Create utils.js with all utility functions**

Port every function from the spec's utils.js Contents table. Read `js/app/namespace.js` for the original implementations.

```js
// js/app/utils.js
'use strict';

export var fetchInterval = 2000;

export var durationToString = function(duration) {
  // Copy from namespace.js lines 63-90
  // Keep EXACTLY as-is — same nanosecond conversion logic
};

export var processLogs = function(text) {
  // Copy from namespace.js lines 92-101
};

export function pikoTimeAgo(dateStr) {
  // Copy from namespace.js lines 54-61
}

export var parseHCLErrors = function(errorStr) {
  // Copy from namespace.js lines 130-164
};

export var blockTypes = [
  // Copy from namespace.js lines 167-177
];

export function toggleTheme() {
  // Copy from namespace.js lines 3-13
}

export function syncThemeSwitch() {
  // Copy from namespace.js lines 15-24
}

export function exportDatabase(jwt) {
  // Adapted from namespace.js lines 26-50
  // Change: accept jwt as parameter instead of reading from window.app.session
  if (!jwt) return;
  fetch('/admin/export', {
    headers: { 'Authorization': 'Bearer ' + jwt }
  }).then(function(resp) {
    if (!resp.ok) {
      return resp.text().then(function(body) { throw new Error(body || resp.statusText); });
    }
    return resp.blob();
  }).then(function(blob) {
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'pikoci.db';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }).catch(function(err) {
    // Will be wired to toast in Layout component
    console.error('Export failed:', err.message);
  });
}

// New utility functions (not in namespace.js)

export function sortBuilds(builds) {
  return [...builds].sort(function(a, b) {
    var pa = a.build_number.split('.');
    var pb = b.build_number.split('.');
    var mainDiff = parseInt(pb[0], 10) - parseInt(pa[0], 10);
    if (mainDiff !== 0) return mainDiff;
    var subA = pa.length > 1 ? parseInt(pa[1], 10) : -1;
    var subB = pb.length > 1 ? parseInt(pb[1], 10) : -1;
    return subB - subA;
  });
}

export function selectActiveBuild(builds, requestedID) {
  if (requestedID) return requestedID;
  var started = builds.filter(function(b) { return b.status === 'started'; });
  var pending = builds.filter(function(b) { return b.status === 'pending'; });
  var running = (started.length ? started[started.length - 1] : null)
    || (pending.length ? pending[pending.length - 1] : null);
  return running ? running.build_number : (builds.length ? builds[0].build_number : null);
}

export function versionRef(v) {
  if (!v) return '';
  if (typeof v === 'string') return v;
  if (v.ref) return v.ref;
  if (v.digest) return v.digest;
  if (v.tag) return v.tag;
  if (typeof v.version === 'string') return v.version;
  for (var key in v) {
    if (v.hasOwnProperty(key)) {
      return key + ': ' + v[key];
    }
  }
  return '';
}
```

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/utils.js
git commit -m "feat: add utils.js with utility functions for Preact migration"
```

---

### Task 3: Core Infrastructure — `state.js`

**Files:**
- Create: `js/app/state.js`

- [ ] **Step 1: Create state.js with Preact Signals**

```js
// js/app/state.js
'use strict';

import { signal, computed } from '@preact/signals';

export const userSessionKey = 'piko-user-jwt';
export const session = signal(JSON.parse(localStorage.getItem(userSessionKey) || '{}'));
export const apiNotice = signal({ error: '', success: '' });
export const teams = signal([]);

export const isLoggedIn = computed(() => !!session.value.jwt);
export const isAdmin = computed(() => {
  const u = session.value.user;
  return u && u.admin;
});

export function isTeamAdmin(tc) {
  const u = session.value.user;
  if (!u) return false;
  if (u.admin) return true;
  return (u.memberships || []).some(m => m.admin && m.team_canonical === tc);
}

export function isTeamMember(tc) {
  const u = session.value.user;
  if (!u) return false;
  if (u.admin) return true;
  if (!tc) return true;
  return (u.memberships || []).some(m => m.team_canonical === tc);
}

export function login(jwt, user) {
  session.value = { jwt, user };
  localStorage.setItem(userSessionKey, JSON.stringify(session.value));
}

export function logout() {
  session.value = {};
  localStorage.removeItem(userSessionKey);
}

export function setNoticeError(msg) {
  apiNotice.value = { error: msg, success: '' };
}

export function setNoticeSuccess(msg) {
  apiNotice.value = { error: '', success: msg };
}

export function clearNotice() {
  apiNotice.value = { error: '', success: '' };
}
```

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/state.js
git commit -m "feat: add state.js with Preact Signals for session management"
```

---

### Task 4: Core Infrastructure — `toast.js`

**Files:**
- Create: `js/app/toast.js`

- [ ] **Step 1: Create toast.js with direct function call pattern**

```js
// js/app/toast.js
'use strict';

var _setToasts = null;

export function registerToastSetter(fn) { _setToasts = fn; }

export function showToast(msg, type) {
  if (!_setToasts) return;
  var id = Date.now();
  _setToasts(function(prev) { return prev.concat([{ id: id, msg: msg, type: type, show: false }]); });
  requestAnimationFrame(function() {
    if (!_setToasts) return;
    _setToasts(function(prev) {
      return prev.map(function(t) { return t.id === id ? Object.assign({}, t, { show: true }) : t; });
    });
  });
  var duration = type === 'error' ? 8000 : 4000;
  setTimeout(function() { dismissToast(id); }, duration);
}

export function dismissToast(id) {
  if (!_setToasts) return;
  _setToasts(function(prev) {
    return prev.map(function(t) { return t.id === id ? Object.assign({}, t, { show: false }) : t; });
  });
  setTimeout(function() {
    if (!_setToasts) return;
    _setToasts(function(prev) { return prev.filter(function(t) { return t.id !== id; }); });
  }, 300);
}
```

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/toast.js
git commit -m "feat: add toast.js notification system for Preact migration"
```

---

### Task 5: Core Infrastructure — `api.js`

**Files:**
- Create: `js/app/api.js`
- Reference: `js/app/init.js`, `js/app/models.js`, `js/app/collections.js` (read, do not modify)

- [ ] **Step 1: Create api.js with fetch wrappers and full endpoint list**

Copy the `api()`, `refreshToken()`, `apiInterval()`, `ApiError` class, and all endpoint functions from the spec's "API Layer" and "Full Endpoint List" sections. This is the largest single file — approximately 120 lines.

Read `js/app/init.js` lines 7-62 for the original Backbone.sync override logic to ensure the error handling, token refresh, and `isInterval` behavior matches exactly.

Read `js/app/models.js` and `js/app/collections.js` for all URL patterns.

The file should import from `./state.js` and `./toast.js`.

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/api.js
git commit -m "feat: add api.js with fetch wrappers and full endpoint list"
```

---

### Task 6: Core Infrastructure — `hooks.js`

**Files:**
- Create: `js/app/hooks.js`

- [ ] **Step 1: Create hooks.js with useRequireAuth and useLoading**

```js
// js/app/hooks.js
'use strict';

import { useState, useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { isLoggedIn, session, isTeamAdmin } from './state.js';

export function useRequireAuth(opts) {
  opts = opts || {};
  var adminOnly = opts.adminOnly || false;
  var teamCanonical = opts.teamCanonical || null;

  useEffect(function() {
    if (!isLoggedIn.value) {
      route('/login');
      return;
    }
    if (session.value.user && session.value.user.must_change_password && window.location.pathname !== '/profile') {
      route('/profile');
      return;
    }
    if (adminOnly && !isTeamAdmin(teamCanonical)) {
      route('/');
    }
  }, []);

  return isLoggedIn.value;
}

export function useLoading() {
  var state = useState(false);
  var loading = state[0];
  var setLoading = state[1];

  function withLoading(fn) {
    setLoading(true);
    return Promise.resolve().then(fn).finally(function() { setLoading(false); });
  }

  return [loading, withLoading];
}
```

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/hooks.js
git commit -m "feat: add hooks.js with useRequireAuth and useLoading"
```

---

### Task 7: Extract `graph-zoom.js`

**Files:**
- Create: `js/app/graph-zoom.js`
- Reference: `js/app/views/editor.js` lines 9-410 (read, do not modify yet)

- [ ] **Step 1: Extract PikoGraphZoom class**

Copy lines 9-410 from `js/app/views/editor.js` into `js/app/graph-zoom.js`. The class is already a standalone vanilla JS class with `export var PikoGraphZoom = function(...)` syntax. No dependencies on jQuery, Backbone, or Underscore. No modifications needed to the class itself.

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/graph-zoom.js
git commit -m "feat: extract PikoGraphZoom to standalone graph-zoom.js"
```

---

### Task 8: Layout Component — `Layout.js`

**Files:**
- Create: `js/app/components/Layout.js`
- Reference: `js/app/views/layout.js`, `templates/views/layouts/index.tmpl` templates `header-view`, `notice-view`, `breadcrumb-view`, `main-view`

- [ ] **Step 1: Create Layout.js**

This file contains 4 components:
1. `Layout` — wraps the app with header, notice, breadcrumb, and main content areas
2. `Header` — navbar with user dropdown, admin links, worker health banner, version display
3. `Notice` — toast notification renderer (uses `registerToastSetter` from toast.js)
4. `Breadcrumb` — breadcrumb navigation (receives context from route components)

Read `js/app/views/layout.js` for all behaviors:
- `HeaderView` fetches `/version` and `/workers/health` on render
- `NoticeView` shows toasts with auto-dismiss (4s success, 8s error)
- `BreadcrumbView` renders team/pipeline/job/resource context
- Theme toggle uses `toggleTheme()` from utils.js

Port all the HTML from the corresponding `<script type="text/template">` blocks in `index.tmpl`.

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/Layout.js
git commit -m "feat: add Layout component with Header, Notice, and Breadcrumb"
```

---

### Task 9: App Entry Point — `app.js` + Login Component

**Files:**
- Create: `js/app/components/Login.js`
- Create: `js/app/app.js` (new entry point — NOT `init.js`)

- [ ] **Step 1: Create Login.js**

Port from `js/app/views/session.js` and the `session-new-view` template.

- [ ] **Step 2: Create app.js with Router**

This is the main entry point. It imports all components, sets up the Router with all routes (from spec's Route Definition section), includes the `NotFound` catch-all, and calls `render()`.

```js
import { html, render } from 'htm/preact';
import { Router, route } from 'preact-router';
// ... all component imports
// ... route definitions from spec
render(html`<${App} />`, document.getElementById('app'));
```

- [ ] **Step 3: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/Login.js js/app/app.js
git commit -m "feat: add app.js entry point with Router and Login component"
```

---

### Task 10: Teams Component

**Files:**
- Create: `js/app/components/Teams.js`
- Reference: `js/app/views/teams.js`, templates `teams-view`, `teams-new-view`, `team-row-view`, `team-show-view`, `team-new-member-row-view`, `team-show-member-row-view`

- [ ] **Step 1: Create Teams.js**

Contains: `TeamsView`, `TeamNew`, `TeamShow` (with member management).

Port all behaviors from `js/app/views/teams.js`:
- Team list with delete
- New team form
- Team show with name edit, member add/remove/admin toggle
- All auth checks (`isAdmin`, `isMember`)

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/Teams.js
git commit -m "feat: add Teams component with list, create, show, member management"
```

---

### Task 11: Pipeline Components — PipelineGraph, PipelineList, PipelineShow

**Files:**
- Create: `js/app/components/PipelineGraph.js`
- Create: `js/app/components/PipelineList.js`
- Create: `js/app/components/PipelineShow.js`
- Create: `js/app/components/PipelineListView.js`
- Reference: `js/app/views/pipelines.js` (1726 lines — the most complex file)

- [ ] **Step 1: Create PipelineGraph.js**

SVG rendering via `window.Viz.instance()`, SVG post-processing (remove background, convert polygons to rounded rects, custom font, clickable nodes), `noLinks` prop for card mode, `PikoGraphZoom` integration via `useRef`/`useEffect`.

- [ ] **Step 2: Create PipelineList.js**

Pipeline card grid. Each card renders a `PipelineGraph` with `noLinks: true` and detects status from SVG fill colors (`#ff004d`, `#ffa300`, `#00a83a`). Live status toggle with localStorage `liveStatusEnabled`. Polling via `useEffect` + `setInterval`.

- [ ] **Step 3: Create PipelineShow.js**

The main pipeline page with graph/list view toggle, resources panel (`PipelineResourcesPanel` sub-component), gear panel, share panel, version tracking banner, pause/unpause/delete actions.

Read `js/app/views/pipelines.js` `PipelineShowView` (lines 405-815) and `PipelineResourcesPanelView` (lines 164-403) carefully.

Key behaviors:
- localStorage: `piko-pipeline-view`, `piko-hide-intermediates`, `piko-group-parallel`
- Version tracking via `?version=` URL param
- `window.history.replaceState()` for query param management
- SVG link interception in `clickPipeline` handler
- Resources panel with expandable version lists

- [ ] **Step 4: Create PipelineListView.js**

The most complex component (~900 lines of logic to port from `PipelineListView` in pipelines.js lines 817-1726).

Key behaviors:
- Chain resolution algorithm (`_findTriggerResources`, `_resolveChain`)
- Tree rendering with parallel groups and fan-in detection
- Resource selector dropdown with outside-click close
- Version selector with recent versions fetch
- Two polling timers (jobs + resources)
- Embedded `JobBuilds` component with `embedded: true`
- localStorage: `piko-list-{pn}-job`, `piko-list-{pn}-resource`, `piko-list-{pn}-collapsed`

- [ ] **Step 5: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/PipelineGraph.js js/app/components/PipelineList.js js/app/components/PipelineShow.js js/app/components/PipelineListView.js
git commit -m "feat: add Pipeline components (graph, list, show, list view)"
```

---

### Task 12: Editor Component

**Files:**
- Create: `js/app/components/Editor.js`
- Reference: `js/app/views/editor.js` (lines 411-983, after PikoGraphZoom extraction)

- [ ] **Step 1: Create Editor.js**

Port `PipelinesNewView` from editor.js. This component is shared between web and local modes via callback props (`onSave`, `onSaveSuccess`).

Key behaviors:
- CodeMirror setup with HCL language, themes, extensions, keybindings
- `MutationObserver` for live theme switching
- Blocks panel with error indicators
- Graph preview with 500ms debounce (two containers: bottom + strip)
- Graph node click → editor block jump
- Tabs (HCL vs vars.json)
- Two fullscreen modes (editor card + graph)
- Escape key handler
- Document click handler for docs menu
- Full cleanup on unmount

Also port the CodeMirror helper functions: `hclLanguage()`, `cmLightTheme()`, `cmDarkTheme()`, `cmHighlightLight()`, `cmHighlightDark()`.

- [ ] **Step 2: Create PipelineNew and PipelineEdit wrapper components**

These wrap `Editor` with web-mode-specific behavior (routing, auth, save via API).

- [ ] **Step 3: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/Editor.js
git commit -m "feat: add Editor component with CodeMirror, blocks panel, graph preview"
```

---

### Task 13: Jobs Component

**Files:**
- Create: `js/app/components/Jobs.js`
- Reference: `js/app/views/jobs.js`

- [ ] **Step 1: Create Jobs.js**

Contains: `JobBuilds`, `BuildTab`, `BuildContent`.

Port all behaviors from `js/app/views/jobs.js`:
- Build tabs with status stripes
- Build content with step rendering (expand/collapse, in_parallel groups)
- Log display with `processLogs()`, copy button
- Auto-scroll/follow mode with manual scroll detection
- Elapsed timer (1s interval)
- Cancel/retry buttons (auth: any logged-in user)
- Pagination (cursor-based `fetchNew` with `newestID`)
- Version tracking: `_fetchTrackedBuilds`, `_filterByTrackedBuildIDs`
- Preserving expanded step state across re-renders
- Embedded mode (`embedded` prop) for list view

Step type → icon mapping: `get`→`bi-cloud-download`, `task`→`bi-terminal`, `put`→`bi-cloud-upload`, `notify`→`bi-bell`, `service`→`bi-hdd-stack`, `runner`→`bi-gear`, job→`bi-braces`.

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/Jobs.js
git commit -m "feat: add Jobs component with build tabs, logs, and version tracking"
```

---

### Task 14: Resources, Users, Workers Components

**Files:**
- Create: `js/app/components/Resources.js`
- Create: `js/app/components/Users.js`
- Create: `js/app/components/Workers.js`
- Reference: `js/app/views/resources.js`, `js/app/views/users.js`, `js/app/views/workers.js`

- [ ] **Step 1: Create Resources.js**

Port `ResourceVersionsView` and `ResourceVersionView`:
- Resource error logs alert
- Webhook panel (admin only) with copy/regenerate
- Version rows: expand/collapse, track, trigger, pin/unpin
- Pinned version banner
- Polling: model (`isInterval`) + collection merge (`remove: false` equivalent)
- Window-level scroll listener for pagination

- [ ] **Step 2: Create Users.js**

Port `UsersListView`, `UsersRowView`, `UserShowView`, `UsersNewView`, `ProfileView`:
- User CRUD with `confirm()` dialogs
- Profile edit with chained `refreshToken` call
- Password change with validation
- `must_change_password` flow

- [ ] **Step 3: Create Workers.js**

Port `WorkersListView` and `WorkersRowView`:
- Version mismatch detection (fetch `/version`, compare against workers)
- Bootstrap tooltip integration via `useEffect` with cleanup
- Delete stale workers
- Outdated worker icon with tooltip

- [ ] **Step 4: Commit**

```bash
git add pikoci/transport/http/assets/js/app/components/Resources.js js/app/components/Users.js js/app/components/Workers.js
git commit -m "feat: add Resources, Users, and Workers components"
```

---

### Task 15: Local Editor Entry Point

**Files:**
- Create: `js/app/local-app.js`

- [ ] **Step 1: Create local-app.js**

Minimal entry point that renders `Notice` + `Editor` without Router or auth. Uses callback props for save behavior (POST to `/local/save` with base64-encoded config).

Copy the pattern from the spec's "Preact equivalent: local-app.js" section.

- [ ] **Step 2: Commit**

```bash
git add pikoci/transport/http/assets/js/app/local-app.js
git commit -m "feat: add local-app.js entry point for pikoci local editor"
```

---

### Task 16: Update HTML Templates and Go Files — The Swap

**Files:**
- Modify: `templates/views/layouts/index.tmpl` (path: `pikoci/transport/http/templates/views/layouts/index.tmpl`)
- Modify: `pikoci/transport/http/local.go`

- [ ] **Step 1: Update index.tmpl**

Replace the entire file with the minimal shell from the spec's "index.tmpl Changes" section:
- Remove all 30 `<script type="text/template">` blocks
- Remove jQuery, Underscore, Backbone script tags
- Add import map
- Add theme init script
- Keep Bootstrap, Viz.js, CodeMirror script tags
- Change entry point to `<script type="module" src="/js/app/app.js"></script>`

- [ ] **Step 2: Update local.go**

Update the `localEditorHTML` constant:
- Remove all `<script type="text/template">` blocks (only had 4)
- Remove jQuery, Underscore, Backbone script tags
- Add import map (same as index.tmpl)
- Change entry point to `<script type="module" src="/js/app/local-app.js"></script>`
- Keep Bootstrap, Viz.js, CodeMirror script tags

- [ ] **Step 3: Commit**

```bash
git add pikoci/transport/http/templates/views/layouts/index.tmpl pikoci/transport/http/local.go
git commit -m "feat: update HTML templates with import map and Preact entry points"
```

---

### Task 17: Delete Old Backbone Files

**Files:**
- Delete: `js/jquery.min.js`
- Delete: `js/underscorejs.min.js`
- Delete: `js/backbonejs.min.js`
- Delete: `js/app/models.js`
- Delete: `js/app/collections.js`
- Delete: `js/app/router.js`
- Delete: `js/app/namespace.js`
- Delete: `js/app/init.js`
- Delete: `js/app/local-init.js`
- Delete: `js/app/views/layout.js`
- Delete: `js/app/views/session.js`
- Delete: `js/app/views/teams.js`
- Delete: `js/app/views/pipelines.js`
- Delete: `js/app/views/editor.js`
- Delete: `js/app/views/jobs.js`
- Delete: `js/app/views/resources.js`
- Delete: `js/app/views/users.js`
- Delete: `js/app/views/workers.js`

- [ ] **Step 1: Delete old files**

```bash
rm pikoci/transport/http/assets/js/jquery.min.js js/underscorejs.min.js js/backbonejs.min.js
rm pikoci/transport/http/assets/js/app/models.js js/app/collections.js js/app/router.js js/app/namespace.js
rm pikoci/transport/http/assets/js/app/init.js js/app/local-init.js
rm -r pikoci/transport/http/assets/js/app/views/
```

- [ ] **Step 2: Commit**

```bash
git add -A
git commit -m "chore: remove old Backbone, jQuery, Underscore files and views"
```

---

### Task 18: Run Integration Tests and Fix

- [ ] **Step 1: Run Selenium integration tests**

```bash
make test-integration
```

Expected: All tests pass. If any fail, debug by:
1. Check `screenshot.png` for visual state at failure
2. Compare CSS selectors in tests against Preact component HTML output
3. Verify all element IDs, classes, and data attributes match exactly
4. Check timing — `waitFor` polls may need longer timeouts for Preact render cycles

- [ ] **Step 2: Run HTTP integration tests**

```bash
make test-http
```

Expected: All pass (no API changes were made).

- [ ] **Step 3: Fix any failing tests**

Iterate until all tests pass. Common issues:
- Missing CSS class on a Preact component
- Different element nesting (Preact may add wrapper divs)
- Timing differences (Preact renders async, may need slightly longer waits)
- Missing `data-*` attributes
- Bootstrap JS initialization timing

- [ ] **Step 4: Commit fixes if any**

```bash
git add -A
git commit -m "fix: resolve integration test failures after Preact migration"
```

---

### Task 19: Manual Verification Against Checklist

- [ ] **Step 1: Walk through the full Verification Checklist**

Open the spec's Verification Checklist (35 items) and manually verify each one in a browser. Mark each item as checked.

Pay special attention to items NOT covered by Selenium tests:
- Graph zoom/pan/fullscreen
- Auto-scroll/follow mode in build logs
- Elapsed timer updates
- CodeMirror live theme switching
- Local editor (requires `pikoci local` command)
- Version tracking `?version=` param survives reload

- [ ] **Step 2: Final commit**

```bash
git commit --allow-empty -m "chore: manual verification of Preact migration complete"
```

---

## Task Dependency Graph

```
Task 1 (vendor libs)
  └→ Task 2 (utils.js)
  └→ Task 3 (state.js)
  └→ Task 4 (toast.js)
      └→ Task 5 (api.js) ← depends on state.js, toast.js
      └→ Task 6 (hooks.js) ← depends on state.js
  └→ Task 7 (graph-zoom.js)
      └→ Task 8 (Layout.js) ← depends on state.js, toast.js, utils.js, api.js
      └→ Task 9 (app.js + Login) ← depends on Layout.js, state.js, hooks.js
          └→ Task 10 (Teams) ← depends on api.js, state.js, hooks.js
          └→ Task 11 (Pipelines) ← depends on PipelineGraph, graph-zoom, api.js
          └→ Task 12 (Editor) ← depends on PipelineGraph, graph-zoom, api.js
          └→ Task 13 (Jobs) ← depends on api.js, utils.js
          └→ Task 14 (Resources, Users, Workers) ← depends on api.js, hooks.js
          └→ Task 15 (local-app.js) ← depends on Editor, toast.js
              └→ Task 16 (HTML swap) ← depends on ALL components
                  └→ Task 17 (delete old files)
                      └→ Task 18 (run tests)
                          └→ Task 19 (manual verification)
```

Tasks 2-7 can be done in parallel. Tasks 10-15 can be done in parallel. Tasks 16-19 are strictly sequential.
