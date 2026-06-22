# Design: Migrate Frontend from BackboneJS to Preact + HTM

**Date:** 2026-06-21
**Status:** Draft
**Motivation:** BackboneJS full re-renders cause visible flickering on model changes. A virtual DOM framework solves this at the framework level. BackboneJS is also effectively unmaintained with a shrinking ecosystem.

## Constraints

- **No build step**: PikoCI's frontend must work without webpack, Vite, Babel, or any transpiler. All JS is served as native ES modules by the Go backend.
- **Keep it small**: PikoCI is intentionally lightweight. The framework choice must reflect this.
- **Plain JS or TypeScript**: Both should be possible, but no requirement for either.
- **No API changes**: The Go REST backend remains unchanged.
- **Minimal Go changes**: `index.tmpl` is a pure HTML file (no Go template directives — `t.Execute(w, nil)` passes nil). The local editor uses a hardcoded `localEditorHTML` constant in `local.go`. Both need updating (script tags + import map) but no Go logic changes.

## Decision: Preact + HTM (Drop Backbone Entirely)

Replace Backbone, jQuery, and Underscore with Preact + HTM + preact-router + Preact Signals.

### Why Preact + HTM

| Criteria | Preact+HTM | Vue 3 | Lit |
|----------|-----------|-------|-----|
| No build step | Native | Works but second-class | Native |
| Bundle size | ~8KB total | ~40KB | ~7KB |
| Flickering solved | Yes (VDOM) | Yes (VDOM) | Yes (targeted updates) |
| AI code generation | Excellent (React-compatible) | Good | Weak |
| Ecosystem/community | Large (React ecosystem) | Large | Small |
| Works with Bootstrap CSS | Yes | Yes | Quirky (Shadow DOM isolation) |
| Plain JS + optional TS | Yes | Yes | TS-leaning |

### Why Drop Backbone Entirely (Not Just Views)

- Backbone Models/Collections are thin REST wrappers — native `fetch()` does the same in fewer lines
- Keeping Backbone for routing/data while using Preact for views creates a split-brain architecture
- Removes jQuery and Underscore dependencies (~100KB)
- AI tools generate much better code for a standard Preact app vs. a Backbone+Preact hybrid
- The codebase is small enough (~5,500 JS lines) for a full rewrite

## Architecture

### Library Stack

| Library | Size | Purpose | Replaces |
|---------|------|---------|----------|
| `preact.module.js` | ~4KB | Virtual DOM rendering | Backbone Views + Underscore templates |
| `preact/hooks` | (included in preact) | `useState`, `useEffect`, `useRef` | Backbone event binding |
| `htm.module.js` | ~1KB | Tagged template JSX alternative (no transpiler) | Underscore `_.template()` |
| `preact-router.module.js` | ~2KB | Client-side routing with `pushState` | Backbone.Router |
| `@preact/signals` | ~1KB | Reactive shared state | Backbone Models (as global state) |

Total: ~8KB replacing ~100KB+ (jQuery 87KB + Backbone 8KB + Underscore 6KB).

All vendored as ES module files in `/js/` — no CDN dependency, same approach as current vendored libs.

### Import Map for Bare Specifiers

Vendored ES modules like `preact-router` and `@preact/signals` use bare import specifiers (e.g., `import { h } from 'preact'`). Browsers cannot resolve bare specifiers without help. We use an **import map** in `index.tmpl` to map these to vendored file URLs:

```html
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
    "@preact/signals": "/js/vendor/signals.module.js"
  }
}
</script>
```

Import maps are supported in all modern browsers (Chrome 89+, Firefox 108+, Safari 16.4+). This allows vendored libraries to use their standard bare specifiers without modification.

Application code uses the same bare specifiers:

```js
import { h } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';
import { html } from 'htm/preact';
import { signal, computed } from '@preact/signals';
import { Router, route } from 'preact-router';
```

### Libraries That Stay

- **Bootstrap 5** (CSS + bundle JS) — dropdowns, modals, collapse, responsive grid
- **Bootstrap Icons** — icon font
- **Viz.js** — Graphviz pipeline graph rendering
- **CodeMirror** — HCL syntax editor

### File Structure

```
js/
  vendor/
    preact.module.js          (vendored)
    preact/
      hooks.module.js         (vendored)
    htm.module.js             (vendored)
    htm-preact.module.js      (vendored, htm bound to preact's h)
    preact-router.module.js   (vendored)
    preact-router-match.module.js (vendored)
    signals.module.js         (vendored)
  bootstrap.bundle.min.js    (existing)
  viz-global.js              (existing)
  codemirror-hcl.min.js      (existing)
  app/
    app.js                   (entry point, App component with Router)
    local-app.js             (entry point for local editor mode)
    api.js                   (fetch wrappers, replaces models.js + collections.js)
    state.js                 (Preact Signals for shared state)
    utils.js                 (utility functions from namespace.js)
    graph-zoom.js            (PikoGraphZoom class, extracted from editor.js as-is)
    components/
      Layout.js              (Header, Notice/Toast, Breadcrumb)
      Login.js               (session/login)
      Teams.js               (team list, create, show)
      PipelineList.js        (pipeline card list with live status)
      PipelineShow.js        (pipeline show: graph/list view, resources panel, gear panel)
      PipelineListView.js    (list view: job tree, resource/version selectors, embedded builds)
      PipelineGraph.js       (Graphviz rendering, version tracking, graph click handling)
      Editor.js              (pipeline HCL editor with CodeMirror, blocks panel)
      Jobs.js                (build history, logs, trigger, cancel, retry)
      Resources.js           (resource versions, pin/unpin, trigger)
      Users.js               (user management, profile)
      Workers.js             (worker status)
```

**Removed files:**
- `jquery.min.js`
- `underscorejs.min.js`
- `backbonejs.min.js`
- `app/models.js`
- `app/collections.js`
- `app/router.js`
- `app/namespace.js` (logic moves to `utils.js`)
- `app/init.js` (replaced by `app.js`)
- `app/local-init.js` (replaced by `local-app.js`)
- All 9 view files in `app/views/`

### utils.js Contents (from namespace.js)

The following utilities from `namespace.js` move to `utils.js`:

| Function | Used by | Notes |
|----------|---------|-------|
| `durationToString(duration)` | Jobs, Builds | Currently called in Build model `parse()`. In Preact, do NOT pre-format — keep durations as raw nanoseconds and format at render time in components. This keeps data pure and avoids lossy string conversion |
| `processLogs(text)` | Jobs (build log templates) | Handles `\r` carriage returns in build logs |
| `pikoTimeAgo(dateStr)` | Pipelines, Workers | Relative time display |
| `parseHCLErrors(errorStr)` | Editor | Parses HCL error strings into diagnostics |
| `blockTypes` | Editor | Block type metadata for the blocks panel |
| `toggleTheme()` / `syncThemeSwitch()` | Layout | Theme toggling with localStorage |
| `exportDatabase()` | Layout (admin) | Blob download of SQLite DB via raw `fetch()` |
| `fetchInterval` | Pipelines, Jobs, Resources | Polling interval constant (2000ms) |
| `sortBuilds(builds)` | Jobs | New — build number comparator (see Build Sorting section) |
| `selectActiveBuild(builds, id)` | Jobs | New — picks the active build to display |
| `versionRef(v)` | Pipelines, Resources | New — extracts human-readable ref from version metadata (currently local to pipelines.js, promote to utils.js) |

Note: `clickLink()` and `addSessionFunctions()` are Backbone-specific helpers and are **not migrated**. `withLoading()` is replaced by the `useLoading` hook (see Loading Button Pattern section) and is **not migrated** to utils.js. — their functionality is handled natively by preact-router and component props.

## API Layer

Replace Backbone's `sync` / Models / Collections with a thin `api.js` module.

```js
import { session, apiNotice, login } from './state.js';

class ApiError extends Error {
  constructor(response, body) {
    super(body?.error || response.statusText || 'Unknown error');
    this.status = response.status;
  }
}

function refreshToken() {
  fetch('/refresh-token', {
    method: 'POST',
    headers: {
      'Authorization': 'Bearer ' + session.value.jwt,
      'Content-Type': 'application/json',
    },
  }).then(r => r.json()).then(resp => {
    if (resp.data && resp.data.jwt) {
      login(resp.data.jwt, resp.data.user);
    }
  }).catch(() => {});
}

async function api(url, opts = {}) {
  const headers = { 'Content-Type': 'application/json' };
  if (session.value.jwt) {
    headers['Authorization'] = 'Bearer ' + session.value.jwt;
  }

  const res = await fetch(url, { ...opts, headers });

  // Handle token refresh
  if (res.headers.get('X-Refresh-Token') === 'true') {
    refreshToken();
  }

  if (!res.ok) {
    const body = await res.json().catch(() => null);
    const err = new ApiError(res, body);

    if (opts.isInterval && apiNotice.value.error !== '') {
      // Already showing an error — don't overwrite during polling
      throw err;
    }
    if (res.status === 0 || res.status === 502 || res.status === 503 || res.status === 504) {
      apiNotice.value = { ...apiNotice.value, error: 'Connection lost. Retrying...' };
    } else {
      apiNotice.value = { ...apiNotice.value, error: err.message };
    }
    throw err;
  }

  if (opts.isInterval) {
    // Interval success: clear error only, preserve success messages
    if (apiNotice.value.error) {
      apiNotice.value = { ...apiNotice.value, error: '' };
    }
  } else {
    apiNotice.value = { error: '', success: '' };
  }

  return res.json();
}

// For interval-based polling: show error on first failure, suppress duplicates.
// On success, clear error but preserve success messages.
// This matches current Backbone.sync isInterval behavior.
async function apiInterval(url, opts = {}) {
  return api(url, { ...opts, isInterval: true });
}
```

### Full Endpoint List

```js
// --- Auth ---
export const postLogin = (data) => api('/login', { method: 'POST', body: JSON.stringify(data) });
export const postRefreshToken = () => api('/refresh-token', { method: 'POST' });
export const fetchVersion = () => api('/version');

// --- Teams ---
export const fetchTeams = () => api('/teams').then(r => r.data);
export const createTeam = (data) => api('/teams', { method: 'POST', body: JSON.stringify(data) }).then(r => r.data);
export const fetchTeam = (tc) => api('/teams/' + tc).then(r => r.data);
export const updateTeam = (tc, data) => api('/teams/' + tc, { method: 'PUT', body: JSON.stringify(data) });
export const deleteTeam = (tc) => api('/teams/' + tc, { method: 'DELETE' });

// --- Team Members ---
export const fetchTeamMembers = (tc) => api('/teams/' + tc + '/members').then(r => r.data);
export const addTeamMember = (tc, data) => api('/teams/' + tc + '/members', { method: 'POST', body: JSON.stringify(data) });
export const updateTeamMember = (tc, username, data) => api('/teams/' + tc + '/members/' + username, { method: 'PUT', body: JSON.stringify(data) });
export const removeTeamMember = (tc, username) => api('/teams/' + tc + '/members/' + username, { method: 'DELETE' });

// --- Pipelines ---
export const fetchPipelines = (tc) => api('/teams/' + tc + '/pipelines').then(r => r.data);
export const fetchPipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn).then(r => r.data);
export const createPipeline = (tc, data) => api('/teams/' + tc + '/pipelines', { method: 'POST', body: JSON.stringify(data) });
export const updatePipeline = (tc, pn, data) => api('/teams/' + tc + '/pipelines/' + pn, { method: 'PUT', body: JSON.stringify(data) });
export const deletePipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn, { method: 'DELETE' });
export const pausePipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/pause', { method: 'POST' });
export const unpausePipeline = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/unpause', { method: 'POST' });

// --- Pipeline Image (Graphviz DOT) ---
// Note: GET returns DOT source; POST previews from raw config (sends byte array)
export const fetchPipelineImage = (tc, pn, params) => {
  const qs = params ? '?' + new URLSearchParams(params) : '';
  return api('/teams/' + tc + '/pipelines/' + pn + '/image.dot' + qs);
};
export const previewPipelineImage = (tc, data) =>
  api('/teams/' + tc + '/pipelines/image.dot', { method: 'POST', body: JSON.stringify(data) });

// --- Jobs ---
export const fetchJobs = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs').then(r => r.data);
export const fetchJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn).then(r => r.data);
export const triggerJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/trigger', { method: 'POST' });
export const pauseJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/pause', { method: 'POST' });
export const unpauseJob = (tc, pn, jn) => api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/unpause', { method: 'POST' });

// --- Builds ---
export const fetchBuilds = (tc, pn, jn, params) => {
  const qs = params ? '?' + new URLSearchParams(params) : '';
  return api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds' + qs);
  // Returns { data, meta: { has_more, newest_id, oldest_id } }
};
export const cancelBuild = (tc, pn, jn, bid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + bid + '/cancel', { method: 'POST' });
export const fetchBuild = (tc, pn, jn, bid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + bid).then(r => r.data);
export const retryBuild = (tc, pn, jn, bid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/jobs/' + jn + '/builds/' + bid + '/retry', { method: 'POST' });

// --- Resources ---
export const fetchResources = (tc, pn) => api('/teams/' + tc + '/pipelines/' + pn + '/resources').then(r => r.data);
export const fetchResource = (tc, pn, rCan) => api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan).then(r => r.data);
export const triggerResource = (tc, pn, rCan) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/trigger', { method: 'POST' });
export const pinResource = (tc, pn, rCan, versionID) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/pin', { method: 'POST', body: JSON.stringify({ version_id: versionID }) });
export const unpinResource = (tc, pn, rCan) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/unpin', { method: 'POST' });

// --- Resource Versions ---
export const fetchResourceVersions = (tc, pn, rCan, params) => {
  const qs = params ? '?' + new URLSearchParams(params) : '';
  return api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions' + qs);
  // Returns { data, meta: { has_more, newest_id, oldest_id } }
};
export const fetchVersionPath = (tc, pn, rCan, vid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions/' + vid + '/path');
export const triggerVersion = (tc, pn, rCan, vid) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/versions/' + vid + '/trigger', { method: 'POST' });

// --- Users ---
export const fetchUsers = () => api('/users').then(r => r.data);
export const fetchUser = (username) => api('/users/' + username).then(r => r.data);
export const createUser = (data) => api('/users', { method: 'POST', body: JSON.stringify(data) });
export const updateUser = (username, data) => api('/users/' + username, { method: 'PUT', body: JSON.stringify(data) });
export const deleteUser = (username) => api('/users/' + username, { method: 'DELETE' });
export const updateProfile = (data) => api('/profile', { method: 'PUT', body: JSON.stringify(data) });
export const changePassword = (data) => api('/users/change-password', { method: 'POST', body: JSON.stringify(data) });

// --- Workers ---
export const fetchWorkers = () => api('/workers').then(r => r.data);
export const fetchWorkersHealth = () => api('/workers/health');
export const deleteWorker = (name) => api('/workers/' + name, { method: 'DELETE' });

// --- Webhooks ---
export const regenerateWebhookToken = (tc, pn, rCan) =>
  api('/teams/' + tc + '/pipelines/' + pn + '/resources/' + rCan + '/webhook_token', { method: 'POST' });

// --- Admin ---
// exportDatabase: GET /admin/export (blob download via raw fetch, kept in utils.js)

// --- Pipeline Image (read-only URLs for share panel) ---
// GET /teams/:tc/pipelines/:pn/image.svg (served by Go, displayed in share panel)
// GET /teams/:tc/pipelines/:pn/image.png (served by Go, displayed in share panel)

// --- Local Editor Mode ---
export const fetchLocalConfig = () => fetch('/local/config').then(r => r.json());
export const saveLocalConfig = (data) => api('/local/save', { method: 'POST', body: JSON.stringify(data) });
```

### Pagination

Builds and ResourceVersions currently use `hasMore`/`oldestID`/`newestID` on Backbone Collections. In Preact, this becomes component-local state:

```js
const [builds, setBuilds] = useState([]);
const [pagination, setPagination] = useState({ hasMore: false, oldestID: 0, newestID: 0 });

async function loadBuilds() {
  const res = await fetchBuilds(tc, pn, jn);
  setBuilds(sortBuilds(res.data));
  setPagination(res.meta);
}

async function loadMore() {
  const res = await fetchBuilds(tc, pn, jn, { before: pagination.oldestID, limit: 50 });
  setBuilds(prev => sortBuilds([...prev, ...res.data]));
  setPagination(res.meta);
}
```

### Build Sorting and Active Selection

The current `Builds` collection has a custom comparator that sorts build numbers as `major.minor` (e.g., `5.1` for retry builds). It also has a `setActive` method. These become utility functions:

```js
// utils.js
export function sortBuilds(builds) {
  return [...builds].sort((a, b) => {
    const pa = a.build_number.split('.');
    const pb = b.build_number.split('.');
    const mainDiff = parseInt(pb[0], 10) - parseInt(pa[0], 10);
    if (mainDiff !== 0) return mainDiff;
    const subA = pa.length > 1 ? parseInt(pa[1], 10) : -1;
    const subB = pb.length > 1 ? parseInt(pb[1], 10) : -1;
    return subB - subA;
  });
}

export function selectActiveBuild(builds, requestedID) {
  if (requestedID) return requestedID;
  const started = builds.filter(b => b.status === 'started');
  const pending = builds.filter(b => b.status === 'pending');
  const running = (started.length ? started[started.length - 1] : null)
    || (pending.length ? pending[pending.length - 1] : null);
  return running ? running.build_number : (builds.length ? builds[0].build_number : null);
}
```

## State Management

### Preact Signals for Shared State

```js
// state.js
import { signal, computed } from '@preact/signals';

export const userSessionKey = 'piko-user-jwt';
export const session = signal(JSON.parse(localStorage.getItem(userSessionKey) || '{}'));
export const apiNotice = signal({ error: '', success: '' });
export const teams = signal([]);

// Computed helpers (replace Session model methods)
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

// API notice helpers
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

### Component-Local State

Most data is component-local (pipeline details, build lists, etc.) — use `useState` and `useEffect` hooks. No need for global stores for page-specific data.

## Routing & Auth Guards

### Route Definition

Auth checks are done per-component, not via a wrapper. Components that require authentication redirect to `/login` in their `useEffect`. Components that support public access (pipeline show, job builds, resource versions) skip the auth check and render regardless.

```js
// app.js
import { html, render } from 'htm/preact';
import { Router } from 'preact-router';
import { Layout } from './components/Layout.js';
import { Login } from './components/Login.js';
import { TeamsView, TeamShow, TeamNew } from './components/Teams.js';
import { PipelineList } from './components/PipelineList.js';
import { PipelineShow } from './components/PipelineShow.js';
import { PipelineNew, PipelineEdit } from './components/Editor.js';
import { JobBuilds } from './components/Jobs.js';
import { ResourceVersions } from './components/Resources.js';
import { WorkersList } from './components/Workers.js';
import { UsersList, UserNew, UserShow } from './components/Users.js';
import { Profile } from './components/Users.js';

function App() {
  return html`
    <${Layout}>
      <${Router}>
        <${Login} path="/login" />
        <${TeamsView} path="/" />
        <${TeamsView} path="/teams" />
        <${TeamNew} path="/teams/new" />
        <${TeamShow} path="/teams/:tc" />
        <${PipelineList} path="/teams/:tc/pipelines" />
        <${PipelineNew} path="/teams/:tc/pipelines/new" />
        <${PipelineShow} path="/teams/:tc/pipelines/:pn" />
        <${PipelineEdit} path="/teams/:tc/pipelines/:pn/edit" />
        <${JobBuilds} path="/teams/:tc/pipelines/:pn/jobs/:jn/builds/:bid?" />
        <${ResourceVersions} path="/teams/:tc/pipelines/:pn/resources/:rCan/versions" />
        <${WorkersList} path="/workers" />
        <${UsersList} path="/users" />
        <${UserNew} path="/users/new" />
        <${UserShow} path="/users/:username" />
        <${Profile} path="/profile" />
        <${NotFound} default />
      </${Router}>
    </${Layout}>
  `;
}

// Catch-all: redirect unknown routes to home (matches current Backbone *notFound handler)
function NotFound() {
  route('/', true);
  return null;
}

render(html`<${App} />`, document.getElementById('app'));
```

### Auth Helper Hook

Used by components that require authentication:

```js
// hooks.js
import { useEffect } from 'preact/hooks';
import { route } from 'preact-router';
import { isLoggedIn, session, isTeamAdmin } from './state.js';

export function useRequireAuth({ adminOnly = false, teamCanonical = null } = {}) {
  useEffect(() => {
    if (!isLoggedIn.value) {
      route('/login');
      return;
    }
    if (session.value.user?.must_change_password && window.location.pathname !== '/profile') {
      route('/profile');
      return;
    }
    if (adminOnly && !isTeamAdmin(teamCanonical)) {
      route('/');
    }
  }, []);
  return isLoggedIn.value;
}
```

### Public Routes

Pipeline show, job builds, and resource versions allow unauthenticated access (for public pipelines). These components do NOT call `useRequireAuth()`. Instead, they check `isLoggedIn` only for conditional UI elements (edit buttons, trigger buttons, etc.).

## Local Editor Mode

The `pikoci local` CLI serves a standalone editor without authentication or routing. Currently implemented in `local-init.js`. The Preact equivalent is `local-app.js`.

### How it works today

1. Go backend serves a **hardcoded HTML string** (`localEditorHTML` constant in `local.go`, NOT a template file). It includes only 4 of the 29+ templates (`main-view`, `notice-view`, `pipeline-graph-view`, `pipelines-new-view`) and loads `local-init.js` instead of `init.js`. No header, footer, breadcrumbs, or non-editor templates
2. `local-init.js` creates a mock session (always admin), overrides Backbone.sync (no auth headers)
3. Fetches `/local/config` to get the initial HCL file contents
4. Creates a fake collection with a custom `create()` method that POSTs base64-encoded config to `/local/save`
5. Renders `PipelinesNewView` directly (no router) — the same editor used in web mode
6. The editor's success callback is intentionally NOT called (it would try to navigate via router which doesn't exist)

### Preact equivalent: `local-app.js`

```js
// local-app.js — entry point for local editor mode
import { html, render } from 'htm/preact';
import { useState, useEffect } from 'preact/hooks';
import { fetchLocalConfig, saveLocalConfig } from './api.js';
import { setNoticeSuccess, setNoticeError } from './state.js';
import { Notice } from './components/Layout.js';
import { Editor } from './components/Editor.js';

function LocalApp() {
  const [config, setConfig] = useState(null);

  useEffect(() => {
    fetchLocalConfig().then(c => setConfig(c));
  }, []);

  if (!config) return null;

  async function onSave(configBytes, vars, name, isPublic) {
    await saveLocalConfig({ config: btoa(String.fromCharCode.apply(null, configBytes)) });
  }

  function onSaveSuccess() {
    setNoticeSuccess('File saved successfully');
    // No navigation — local mode stays on editor
  }

  return html`
    <main class="container">
      <${Notice} />
      <${Editor}
        pipeline=${{ raw: config.raw, name: config.name, canonical: null }}
        teamCanonical="local"
        isLocal=${true}
        onSave=${onSave}
        onSaveSuccess=${onSaveSuccess}
      />
    </main>
  `;
}

render(html`<${LocalApp} />`, document.getElementById('app'));
```

### Local Editor HTML (Go Backend)

The Go backend currently serves local mode via a hardcoded `localEditorHTML` constant in `local.go` (not a separate template file). For the migration, this constant must be updated to:

1. Remove jQuery, Underscore, Backbone script tags
2. Add the import map (same as main `index.tmpl`)
3. Remove all embedded `<script type="text/template">` blocks
4. Change the entry point to `local-app.js`:

```html
<script type="module" src="/js/app/local-app.js"></script>
```

The import map is identical to the main template — the same vendored libraries are used. The only difference is the entry point script.

## Version Tracking Flow

The current codebase has a "version tracking" feature that highlights the path of a specific resource version through the pipeline graph. This spans multiple components:

### Current Behavior

1. User clicks a resource version (from resource versions page or graph node)
2. Router stores `_trackedVersionID` and navigates to pipeline show
3. `PipelineShowView` fetches the version path via `/resources/:rCan/versions/:vid/path`
4. The path response lists which jobs consumed this version
5. Graph edges for those jobs are highlighted
6. If navigating to job builds, the `?version=` query param filters builds to only those related to that version

### Preact Implementation

Version tracking state flows via URL query parameters (`?version=123`), not component state:

```
PipelineShow reads ?version= from URL
  → fetches version path via fetchVersionPath()
  → passes highlighted edges to PipelineGraph
  → PipelineGraph applies CSS classes to SVG edges

JobBuilds reads ?version= from URL
  → filters/highlights builds related to that version

Navigation between pipeline show and job builds preserves ?version= in the URL
```

This is cleaner than the current approach (which uses a transient `_trackedVersionID` property on the router instance). URL query params survive page reloads and are shareable.

## index.tmpl Changes

The Go template becomes a minimal shell with an import map:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <title>PikoCI</title>
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
    <!-- Theme init before module loads (same as current) -->
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
        "@preact/signals": "/js/vendor/signals.module.js"
      }
    }
    </script>

    <script type="text/javascript" src="/js/bootstrap.bundle.min.js"></script>
    <script type="text/javascript" src="/js/viz-global.js"></script>
    <script type="text/javascript" src="/js/codemirror-hcl.min.js"></script>
    <script type="module" src="/js/app/app.js"></script>
  </body>
</html>
```

All 30 embedded `<script type="text/template">` blocks are deleted — markup now lives inside Preact components.

## Third-Party Integrations

### Viz.js (Graphviz)

Currently used via `window.Viz` global. Stays the same — accessed from Preact via `useRef` + `useEffect`:

```js
function PipelineGraph({ dotSource, highlightedEdges }) {
  const containerRef = useRef(null);

  useEffect(() => {
    if (!dotSource || !containerRef.current) return;
    window.Viz.instance().then(viz => viz.renderSVGElement(dotSource)).then(svg => {
      containerRef.current.innerHTML = '';
      containerRef.current.appendChild(svg);
      // Apply version tracking highlights if present
      if (highlightedEdges) applyEdgeHighlights(svg, highlightedEdges);
    });
  }, [dotSource, highlightedEdges]);

  return html`<div ref=${containerRef}></div>`;
}
```

### PikoGraphZoom

The `PikoGraphZoom` class (~400 lines) handles zoom/pan/fullscreen of SVG pipeline graphs. It is framework-agnostic vanilla JS managing mouse, touch, and keyboard events on SVG viewBox attributes. **It does not need rewriting.** Extract it to `graph-zoom.js` and use via `useRef`/`useEffect`:

```js
useEffect(() => {
  if (!svgContainer.current) return;
  const zoom = new PikoGraphZoom(svgContainer.current, options);
  return () => zoom.destroy();
}, []);
```

### CodeMirror (HCL Editor)

Same approach — `useRef` + `useEffect` to initialize the editor instance:

```js
function HCLEditor({ value, onChange, diagnostics }) {
  const editorRef = useRef(null);
  const cmRef = useRef(null);

  useEffect(() => {
    if (!editorRef.current) return;
    cmRef.current = new EditorView({
      doc: value,
      extensions: [/* HCL language, theme, etc. */],
      parent: editorRef.current,
    });
    return () => cmRef.current?.destroy();
  }, []);

  useEffect(() => {
    if (cmRef.current && diagnostics) {
      // apply diagnostic markers
    }
  }, [diagnostics]);

  return html`<div ref=${editorRef} class="piko-editor"></div>`;
}
```

### Bootstrap JS

Bootstrap JS (dropdowns, collapse) modifies the DOM directly, which can conflict with Preact's virtual DOM diffing. Guidelines:

- **Data-attribute initialization** (e.g., `data-bs-toggle="dropdown"`) is safe — Bootstrap creates its own elements outside Preact's tree (dropdown menus appended to body).
- **Programmatic Bootstrap usage** (e.g., `new bootstrap.Tooltip(el)` in workers) must be wrapped in `useEffect` with cleanup to prevent memory leaks:
  ```js
  useEffect(() => {
    const tooltips = containerRef.current.querySelectorAll('[data-bs-toggle="tooltip"]');
    const instances = [...tooltips].map(el => new bootstrap.Tooltip(el));
    return () => instances.forEach(t => t.dispose());
  });
  ```
- **Avoid Preact re-rendering elements that Bootstrap manages.** Keep Bootstrap-managed elements stable (e.g., don't conditionally render a dropdown container based on state).

## Header: Worker Health Banner

The current `HeaderView` fetches `/workers/health` on every render for admin users and displays a warning banner when no healthy workers are detected. This must be preserved in `Layout.js`:

```js
// Inside Header component
function Header() {
  const [workerWarning, setWorkerWarning] = useState(false);

  useEffect(() => {
    if (!isAdmin.value) return;
    fetchWorkersHealth()
      .then(res => setWorkerWarning(!res.error && !res.healthy))
      .catch(() => {});
  }, []);

  return html`
    <nav class="navbar navbar-expand-md">
      <!-- ... navbar content ... -->
    </nav>
    ${workerWarning && html`
      <div class="alert alert-warning text-center mb-0">
        <i class="bi bi-exclamation-triangle"></i> No healthy workers detected.
      </div>
    `}
  `;
}
```

## Toast Notification System

The current `NoticeView.showToast()` creates floating toast notifications that auto-dismiss. In Preact, use a direct function call pattern instead of signal-watching:

```js
// toast.js — module-level state, no signal intermediary
let _setToasts = null;

export function registerToastSetter(fn) { _setToasts = fn; }

export function showToast(msg, type) {
  if (!_setToasts) return;
  const id = Date.now();
  _setToasts(prev => [...prev, { id, msg, type, show: false }]);
  requestAnimationFrame(() => {
    _setToasts(prev => prev.map(t => t.id === id ? { ...t, show: true } : t));
  });
  const duration = type === 'error' ? 8000 : 4000;
  setTimeout(() => dismissToast(id), duration);
}

function dismissToast(id) {
  if (!_setToasts) return;
  _setToasts(prev => prev.map(t => t.id === id ? { ...t, show: false } : t));
  setTimeout(() => _setToasts(prev => prev.filter(t => t.id !== id)), 300);
}
```

The `api.js` layer calls `showToast('Connection lost...', 'error')` directly instead of going through a signal. The `Notice` component registers its setter on mount:

```js
// In Layout.js
function Notice() {
  const [toasts, setToasts] = useState([]);

  useEffect(() => {
    registerToastSetter(setToasts);
    return () => registerToastSetter(null);
  }, []);

  return html`
    ${toasts.map(t => html`
      <div class="piko-toast piko-toast-${t.type} ${t.show ? 'show' : ''}" key=${t.id}>
        ${t.msg}
        <button class="piko-toast-close" onClick=${() => dismissToast(t.id)} />
      </div>
    `)}
  `;
}
```

This removes the signal-watching-useEffect dance and makes the toast flow direct: `api error → showToast() → component re-renders`.

## Loading Button Pattern

The current `withLoading()` utility uses jQuery DOM manipulation. In Preact, this becomes a `useLoading` hook:

```js
// hooks.js
export function useLoading() {
  const [loading, setLoading] = useState(false);

  async function withLoading(fn) {
    setLoading(true);
    try {
      return await fn();
    } finally {
      setLoading(false);
    }
  }

  return [loading, withLoading];
}

// Usage in a component:
function TriggerButton({ onTrigger }) {
  const [loading, withLoading] = useLoading();

  return html`
    <button
      class="btn btn-sm btn-outline-primary"
      disabled=${loading}
      onClick=${() => withLoading(onTrigger)}
    >
      ${loading
        ? html`Loading... <span class="spinner-border spinner-border-sm"></span>`
        : 'Trigger'}
    </button>
  `;
}
```

## Polling / Live Updates

Current pattern: `setInterval` with Backbone `fetch()` and `isInterval` flag.

### Polling Strategies by Component

Different components poll differently — preserve these patterns:

| Component | What is polled | Strategy |
|-----------|---------------|----------|
| Pipeline cards | Pipeline image (graph SVG) | Full re-fetch on interval |
| Pipeline show | Pipeline image + version path | Full re-fetch on interval |
| Pipeline resources panel | Resources collection | Full re-fetch, targeted DOM updates for expanded resources |
| Pipeline list view | Jobs + resources | Full re-fetch on interval (two timers) |
| Job builds | Builds collection | Cursor-based: `fetchNew()` with `{ after: newestID }` — appends new builds |
| Resource versions | Resource model + versions collection | Merge: model re-fetched with `isInterval`, collection re-fetched with `remove: false` (adds new versions but preserves existing — NOT a full replace, NOT cursor-based like builds) |

The key difference: **Builds** use cursor-based polling (fetch only new items). **Resource versions** do a full re-fetch each time.

New pattern: `useEffect` with `setInterval` and cleanup:

```js
useEffect(() => {
  const id = setInterval(() => {
    fetchBuilds(tc, pn, jn, { after: newestID })
      .then(res => {
        if (res.data.length) {
          setBuilds(prev => sortBuilds([...res.data, ...prev]));
          setNewestID(res.meta.newest_id);
        }
      })
      .catch(() => {}); // swallow interval errors silently
  }, fetchInterval);
  return () => clearInterval(id);
}, [newestID]);
```

## localStorage Keys

All current `localStorage` usage must be preserved:

| Key | Used by | Purpose |
|-----|---------|---------|
| `piko-theme` | Layout (theme toggle) | `"light"` or `"dark"` |
| `piko-user-jwt` | Auth (state.js) | JSON: `{jwt, user}` |
| `liveStatusEnabled` | PipelineList | `"true"` or absent — live status polling on pipeline cards |
| `piko-pipeline-view` | PipelineShow | `"graph"` or `"list"` — preferred view mode |
| `piko-list-{pn}-job` | PipelineShow (list view) | Selected job name in list view |
| `piko-list-{pn}-resource` | PipelineShow (list view) | Selected resource canonical in list view |
| `piko-list-{pn}-collapsed` | PipelineShow (list view) | JSON object of collapsed parallel group states |
| `piko-hide-intermediates` | PipelineShow (gear panel) | `"1"` or `"0"` — hide intermediate resources in graph |
| `piko-group-parallel` | PipelineShow (gear panel) | `"1"` or `"0"` — group parallel jobs in graph |

## Editor: Shared Between Web and Local Modes

The editor (`Editor.js`) is the most complex component and is shared between two entry points:

### Web mode (`app.js` → PipelineNew / PipelineEdit)
- Has a router — on save, navigates to the pipeline show page
- Has auth — shows/hides controls based on `isTeamAdmin(tc)`
- Pipeline create: POSTs to `/teams/:tc/pipelines`
- Pipeline update: PUTs to `/teams/:tc/pipelines/:pn`
- Graph preview: POSTs to `/teams/:tc/pipelines/image.dot`
- Has collection context (for pipeline name uniqueness check)

### Local mode (`local-app.js`)
- No router — on save, shows success toast (does NOT navigate)
- No auth — always shows all controls (admin-like)
- Save: POSTs base64-encoded config to `/local/save`
- Graph preview: POSTs to `/teams/local/pipelines/image.dot` (same endpoint pattern)
- No collection — single pipeline only

### Abstraction pattern

The `Editor.js` component accepts props to handle both modes:

```js
function Editor({
  pipeline,           // initial pipeline data (name, raw, canonical)
  teamCanonical,      // team for API URLs (web) or "local" (local)
  isLocal,            // true = local mode
  onSave,             // callback: (configBytes, vars, name, isPublic) => Promise
  onSaveSuccess,      // callback: (pipeline) => void (web navigates, local shows toast)
}) { ... }
```

The editor component itself does NOT know about routing or auth. It delegates:
- **Save behavior** to `onSave` / `onSaveSuccess` callbacks
- **Auth checks** to the parent (web checks `isTeamAdmin`, local always allows)

This keeps the editor reusable without `isLocal` conditionals scattered through the code.

## Editor: Internal Features (Easy to Miss)

The editor has many features that must all be ported:

### CodeMirror Setup
- Custom HCL language definition with keywords, atoms, comments, strings
- Light and dark themes using CSS variables
- Extensions: line numbers, active line highlight, bracket matching, close brackets, indent on input, fold gutter, lint gutter, search, selection matches, history
- Custom keybindings: closeBrackets, default, search, history keymaps + `indentWithTab`
- Theme live-switching via `MutationObserver` on `document.documentElement` `data-theme` attribute

### Blocks Panel
- Parses the editor document to extract block definitions (resource_type, resource, job, etc.)
- Shows each block with a colored icon and name
- Marks blocks that have errors (red indicator)
- Click a block to jump to its position in the editor (selects the line)
- Collapsible with toggle button

### Graph Preview
- Two containers: bottom overlay + side strip (both show the same graph)
- 500ms debounce after each edit before re-rendering
- POSTs current config + vars to `/image.dot` endpoint
- Uses `PipelineGraphView` + `PikoGraphZoom` for rendering
- Clicking a graph node jumps to the corresponding block in the editor
  - Node names like `job--key` are stripped to `job` for the search
- Error responses display inline diagnostics in the editor

### Tabs
- `pipeline.hcl` tab: Shows CodeMirror editor
- `vars.json` tab: Shows plain textarea for variable JSON

### Fullscreen Mode
- Editor card can go fullscreen (class on body)
- Graph can go fullscreen independently (PikoGraphZoom overlay)
- Both close with Escape key

### Cleanup on unmount
- Clear debounce timer
- Disconnect MutationObserver
- Destroy CodeMirror editor
- Destroy PikoGraphZoom instances
- Remove Escape key handler
- Remove document click handler (docs menu)
- Remove fullscreen class from body

## Build Logs: Internal Features (Easy to Miss)

### Step Rendering
- Steps render as expandable rows (click header to toggle body)
- `in_parallel` groups show sub-steps nested inside
- Each step shows: name, status icon, duration
- Step bodies contain `<pre>` elements with log output
- Logs are processed through `processLogs()` to handle `\r` carriage returns
- Step type → icon mapping: `get` → `bi-cloud-download`, `task` → `bi-terminal`, `put` → `bi-cloud-upload`, `notify` → `bi-bell`, `service` → `bi-hdd-stack`, `runner` → `bi-gear`, job steps → `bi-braces`, default → `bi-terminal`

### Auto-scroll / Follow Mode
- "Follow" toggle button: when ON, auto-scrolls to bottom of active log
- Detects manual scroll: if user scrolls up, disables follow
- If user scrolls back near bottom, re-enables follow
- "Go to bottom" button appears when logs overflow and user has scrolled up

### Elapsed Timer
- For running builds (status = `started`): updates elapsed time every 1 second
- Shows `started_at` and `last_update_at` as relative time (`pikoTimeAgo`), also updating every 1 second

### Build Actions
- Cancel button: visible for `started` or `pending` builds, **only if user is authenticated** (any logged-in user, not just admin/member — checks `!session.isEmpty()`)
- Retry button: visible for all non-running statuses (`succeeded`, `failed`, `cancelled`), **only if user is authenticated**
- Both use `withLoading()` pattern

### Clipboard
- Copy logs button per step: copies `<pre>` text content to clipboard
- Shows checkmark icon briefly after copy

### Preserving State Across Re-renders
- Expanded step state is preserved when build data re-fetches
- Active build tab is preserved across polling updates

## Pipeline Show: Internal Features (Easy to Miss)

### View Modes
- Graph view (default): Full pipeline graph with PikoGraphZoom
- List view: Left panel with job list, right panel with embedded `JobBuildsView`
- Preference stored in `localStorage` key `piko-pipeline-view`
- In list view, selected job stored in `localStorage` key `piko-list-{pn}-job`

### Resources Panel
- Sidebar showing all resources with latest version ref and status dot
- "Check" button per resource (triggers resource check, if member)
- Polls for updates on the same fetchInterval (2s)
- Clicking a resource navigates to resource versions page

### Gear Panel
- Toggleable panel with checkboxes:
  - Hide intermediates (passes `hide_intermediates=1` to image.dot)
  - Group parallel (passes `group_parallel=1` to image.dot)
- Re-renders graph when toggled

### Share Panel
- Shows URLs for SVG, PNG, Markdown embed
- Copy button per URL (clipboard API)

### Pipeline Actions (admin only)
- Pause/unpause pipeline
- Edit (navigates to editor)
- Delete (confirmation dialog, then DELETE + navigate to pipelines list)

### Version Tracking Banner
- If `?version=` in URL: shows banner with resource name, version ref, and "clear" button
- Graph edges are highlighted for the tracked version's path

### Graph Node Click
- In graph view: clicking a job node navigates to job builds page
- In list view: clicking a job node selects it in the list panel

### jobBuilds Route + List View Redirect
- If user navigates to `/teams/:tc/pipelines/:pn/jobs/:jn/builds` but their preference is list view:
  - Stores the job name in localStorage
  - Redirects to pipeline show page (list view picks up the job)

## Pipeline List View: Internal Architecture (Complex)

`PipelineListView` is the most complex view in the codebase (~900 lines). It renders when the user switches from graph view to list view on the pipeline show page. It must be ported as part of `PipelineShow.js` (or a separate `PipelineListView.js` component).

### What it does

Shows a left panel with a tree of jobs (respecting parallel groups and fan-in) and a right panel with an embedded `JobBuildsView` for the selected job. Above the panels, a resource selector bar lets users scope the view to a specific resource's versions.

### Chain Resolution Algorithm

The list view computes which jobs are "downstream" of a selected trigger resource:

1. `_findTriggerResources()`: Scans pipeline jobs for `get` steps where `trigger: true` and no `passed` constraint — these are the root trigger resources
2. `_resolveChain()`: BFS from trigger resources through jobs via `passed` constraints. Builds a set of "chain jobs" — the jobs that would run when a resource version triggers
3. When a resource is selected, the job list filters to only show chain jobs

### Tree Rendering

The job list is rendered as a tree, not a flat list:

1. `_buildTree()`: Analyzes parent-child relationships via `passed` constraints
2. `_renderJobList()`: Recursively renders the tree with:
   - Parallel group detection (multiple jobs at the same depth with same parent)
   - Fan-in detection (jobs with multiple parents)
   - Collapsible parallel groups (state stored in localStorage)
3. `_renderParallelGroup()`: Renders collapsed/expanded parallel group with header showing job count

### Resource Selector

- Dropdown above job list showing available trigger resources
- Selecting a resource scopes the job list to its chain
- Stores selected resource in `localStorage` key `piko-list-{pn}-resource`

### Version Selector

- After selecting a resource, a version dropdown appears showing last 10 versions
- `_fetchRecentVersions()`: AJAX GET to fetch versions with `?limit=10`
- Selecting a version activates version tracking (same as graph view)
- Calls parent's `trackVersion()` to apply version scope

### Polling

- Two `setInterval` timers:
  1. Jobs polling: re-fetches job list every `fetchInterval` ms
  2. Resources polling: re-fetches resource data every `fetchInterval` ms
- `pausePolling()` / `resumePolling()` methods for cleanup

### State Persistence (localStorage)

- Selected job: `piko-list-{pn}-job`
- Selected resource: `piko-list-{pn}-resource`
- Collapsed groups: `piko-list-{pn}-collapsed` (JSON object)

### Embedded JobBuildsView

When a job is selected, creates a `JobBuildsView` instance in the right panel with `embedded: true` flag. The embedded flag adjusts the builds view behavior (no separate URL navigation).

## Pipeline Cards: Live Status Detection

`PipelinesCardView` detects pipeline status from the rendered SVG graph colors using DOM queries:

```js
// Checks SVG fill colors to determine pipeline status
const svgEl = this.el.querySelector('svg');
const hasRed = svgEl.querySelector('[fill="#ff004d"]');     // failed
const hasOrange = svgEl.querySelector('[fill="#ffa300"]');   // running
const hasGreen = svgEl.querySelector('[fill="#00a83a"]');    // succeeded
```

This technique must be preserved. The `PipelineGraph` component should expose a callback or use signals to report the detected status, avoiding the need to query SVG internals from the card component.

## Resources Panel: Expandable Version Lists

The `PipelineResourcesPanelView` in the resources side panel supports expanding each resource to show its last 5 versions inline:

- Toggle button with chevron icon per resource card
- `_fetchPanelVersions()`: AJAX GET to fetch last 5 versions for a resource
- Expanded state tracked in `_expandedResources` object
- When expanded resources exist, uses targeted DOM updates (`_updateResourceCards()`) instead of full re-render to avoid flickering — this is exactly the kind of optimization that Preact's VDOM handles automatically
- Panel versions support track, trigger, and pin actions inline

## Resource Versions: Internal Features (Easy to Miss)

### Resource Error Logs
- If `resource.logs` is non-empty, displays an `alert alert-danger` at the top of the page showing error output from failed resource checks

### Webhook Panel
- Admin-only panel showing the webhook URL for the resource
- Copy button (clipboard API)
- Regenerate token button (POSTs to `/webhook_token`, updates resource model)

### Version Row Features
- Collapsible: click to show full version metadata (key-value table)
- Track button: navigates to pipeline show with `?version={id}`
- Trigger button: triggers downstream jobs with this specific version
- Pin/unpin button: pins resource to this version (shows pinned badge)
- Status dot: updates in real-time via polling
- Badges: "latest" (first version), "pinned", "tracked"

### Pinned Version Banner
- Shows at top when resource has a pinned version
- Displays pinned version's metadata
- "Unpin" button

## Workers: Internal Features (Easy to Miss)

### Version Mismatch Banner
- Fetches `/version` on init to get server version + commit
- Compares each worker's version/commit against server
- Shows warning banner if any worker is outdated
- Outdated workers show an icon with Bootstrap tooltip (programmatic `new bootstrap.Tooltip()`)

### Worker Row
- Shows: name, status (healthy/stale with colored dot), tags, platform (os/arch/go_version), version, uptime, last seen
- Delete button only appears for stale workers

## Confirmation Dialogs

The current code uses native `confirm()` before destructive actions:
- Delete pipeline: `confirm("Are you sure you want to delete Pipeline '...")`
- Delete user: `confirm("Are you sure you want to delete user '...")`

In Preact, continue using native `confirm()` — no need for a custom modal component. This keeps things simple.

## SVG Post-Processing in PipelineGraphView

The `PipelineGraphView` applies several transformations to the Graphviz SVG output after rendering:
- Removes white background polygons
- Converts 4-point polygons to rounded rectangles (`<rect>` with `rx`/`ry` attributes)
- Applies custom font family
- Makes nodes clickable (`cursor: pointer`)
- Optionally removes `xlink:href` from nodes (when `noLinks: true` — used by pipeline cards to prevent navigation)

The `PipelineGraph` component must accept a `noLinks` prop for card rendering mode.

## Debug Global

The current editor stores `window._pikoEditor = this.editor` (CodeMirror instance) as a debug global. This is not essential and can be dropped in the Preact version.

## Inline onclick Handlers in Templates

The current templates use inline `onclick` handlers for several interactions. These must be converted to Preact event handlers:

| Template | Inline Handler | Preact Equivalent |
|----------|---------------|-------------------|
| job-builds-content-view | Step row toggle (`var body=this.nextElementSibling...`) | `onClick` on step header component |
| job-builds-content-view | Copy logs (`navigator.clipboard.writeText(...)`) | `onClick` handler with clipboard API |
| job-builds-content-view | Goto bottom (`pre.scrollTop=pre.scrollHeight`) | `onClick` handler with ref |
| resource-version-view | Version row toggle (expand/collapse metadata table) | `onClick` on version row header component |
| header-view | Theme toggle (`toggleTheme()`) | `onClick` in Header component |
| header-view | Export database (`exportDatabase()`) | `onClick` in Header component |

## Pre-Migration: Selenium Test Gap Analysis

The existing Selenium tests at `integration/selenium/pikoci_test.go` cover the core happy paths but have significant gaps in build log details, resource management, admin pages, editor features, and edge cases. Before starting the migration, add tests for uncovered features to establish a baseline. All tests must pass before AND after the migration.

### Current Test Status

All tests pass except one pre-existing failure:
- `Admin/Resources_Panel` — line 492: `.piko-resource-card-type` text is empty (the `r.type` field may not be populated by the API for this resource). Investigate and fix independently.

### Existing Coverage (COVERED — no action needed)

- Login/logout flow + must_change_password redirect
- Teams: list, create, show, edit, delete, member add/remove/admin toggle
- Pipeline graph renders (SVG appears)
- Pipeline list view: job tree visible, resource selector present
- Pipeline resources panel: resource cards display, check button visible
- Pipeline gear panel: hide intermediates + group parallel toggle buttons exist
- Pipeline share panel: SVG/PNG/Markdown URLs populated, copy buttons exist
- Pipeline delete with confirmation dialog
- Editor: create new pipeline, edit existing, graph preview renders
- Version tracking: expand resource → track → banner appears → clear
- Resources page: trigger resource, versions appear
- Job builds: build tabs appear, trigger job increases count
- Pipeline card timestamp updates (live polling)
- Toast notifications (password change toast verified)
- Breadcrumb navigation (used for page verification throughout)
- Token refresh (X-Refresh-Token header, async permission sync)
- Public pipeline: view graph, switch views, view builds, no trigger/cancel/retry/check buttons
- Member access control: no create/edit/delete buttons, disabled inputs, redirect on direct URL
- Admin export database link visible/hidden based on role

### Existing Coverage Weaknesses (tested but shallow)

These are tested but only check for element existence, not functional behavior:

- **Gear panel**: Toggles open/close but does NOT verify graph re-renders with different nodes after checking hide intermediates
- **Share panel**: URLs found but copy buttons never clicked — clipboard not verified
- **Resources panel**: Card found but check button never actually clicked to verify it works
- **Build tabs**: Tabs counted but content (steps, logs) never verified
- **Edit pipeline**: Form submitted but pre-fill of existing data not verified
- **Pipeline cards**: Timestamp checked but card name/public badge not verified
- **Delete operations**: Confirm dialog accepted but dialog text not verified
- **Toast messages**: Only password change toast verified — team/pipeline CRUD operations don't check toasts

### Tests to Add Before Migration

#### Priority 1: Build Logs (most complex UI, highest migration risk)

```
Admin/Build_Logs/Steps_Render
  - Navigate to job builds, wait for completed build
  - Click build tab to view content
  - Find step rows (.piko-step-row), verify at least 1 exists
  - Find step header (.piko-step-row-header), verify step name text present
  - Verify step shows duration text

Admin/Build_Logs/Steps_Expand_Collapse
  - Find step header (.piko-step-row-header), click to expand
  - Verify step body (.piko-step-row-body) display changes to visible
  - Verify <pre> element with log text is present inside body
  - Click header again to collapse, verify body hidden

Admin/Build_Logs/Copy_Logs_Button
  - Find copy logs button (.piko-copy-logs-header-btn) inside expanded step
  - Verify button exists (clipboard API can't be tested easily, verify presence)

Admin/Build_Logs/Cancel_Retry_Buttons_Auth
  - Trigger a job, wait for build with status "started" or "succeeded"
  - If started: verify cancel button (.piko-cancel-build) visible for logged-in user
  - If completed: verify retry button (.piko-retry-build) visible for logged-in user
  - Navigate to same build as public (logged out) → verify neither button visible

Admin/Build_Logs/Build_Status_Transition
  - Trigger a job, wait for new build tab
  - Wait for build to complete (poll build tab status class changes)
  - Verify final status class on tab (e.g., piko-status-succeeded or piko-status-failed)
```

#### Priority 2: Resource Pin/Unpin, Webhook, and Version Row

```
Admin/Resource_Versions/Version_Row_Expand
  - Navigate to resource versions page with at least 1 version
  - Click version row header (.piko-version-row-header)
  - Verify version row body (.piko-version-row-body) becomes visible
  - Verify body contains version metadata table (.piko-version-table)

Admin/Resource_Versions/Pin_Unpin
  - Find pin button (.pin-version) on a version row
  - Click pin, verify button style changes (btn-warning class)
  - Verify pinned banner (#pinned-version-banner) appears
  - Click unpin button (#unpin-banner), verify banner hides
  - Verify pin button returns to outline style

Admin/Resource_Versions/Webhook_Panel
  - Navigate to resource versions page (as admin)
  - Find webhook toggle (#toggle-webhook-panel) in dropdown
  - Click to open, verify webhook panel (#webhook-panel) is visible
  - Verify webhook URL (#webhook-url) is populated (non-empty text)
  - Find copy button (#copy-webhook) — verify exists
  - Find regenerate button (#regenerate-webhook) — click it
  - Verify webhook URL changes after regeneration

Member/Resource_Versions/No_Webhook_Panel
  - Navigate to resource versions page as member (non-admin)
  - Verify webhook toggle (#toggle-webhook-panel) does NOT exist
```

#### Priority 3: Users Page (entirely untested)

```
Admin/Users/Navigate_And_List
  - Click navbar dropdown, click Users link (#nav-users)
  - Verify h1 heading "Users"
  - Verify users table renders with rows (at least admin, pepito, grillo)
  - Verify each row shows username, full_name, role badge

Admin/Users/Create_User
  - Click new user button (#user-new)
  - Verify h1 "New User"
  - Fill form: username, full_name, password
  - Submit form
  - Verify redirect to user detail page or users list
  - Verify new user appears in list

Admin/Users/Edit_User
  - Click on user row link
  - Verify h1 "Edit User: <username>"
  - Update full_name (#full_name), submit
  - Verify success (toast or updated breadcrumb)

Admin/Users/Reset_Password
  - On user detail page, find reset password form (#reset-password-form)
  - Enter new password (#new_password)
  - Submit
  - Verify success feedback

Admin/Users/Delete_User
  - Click delete button (#delete-user)
  - Accept confirmation dialog
  - Verify redirect to users list
  - Verify user no longer in list
```

#### Priority 4: Workers Page (entirely untested)

```
Admin/Workers/Navigate_And_List
  - Click navbar dropdown, click Workers link (#nav-workers)
  - Verify h1 heading "Workers"
  - Verify workers table renders
  - Verify at least 1 worker row (test-worker-1 from test setup)
  - Verify worker shows name, status, version columns
```

#### Priority 5: Editor Advanced Features

```
Admin/Editor/Vars_Tab
  - Navigate to pipeline editor (new or edit)
  - Click vars.json tab (#tab-vars)
  - Verify vars textarea (#vars) is visible (display != none)
  - Verify code area (#code-area) is hidden
  - Click pipeline.hcl tab (#tab-hcl)
  - Verify code area is visible, vars area is hidden

Admin/Editor/Blocks_Panel
  - Navigate to pipeline editor, enter HCL with resource + job
  - Wait for blocks panel (#blocks-panel) to populate
  - Find block items (.piko-blocks-item)
  - Verify at least 2 blocks (resource + job)
  - Click a block item
  - Verify editor scrolls (block panel interaction works without error)

Admin/Editor/Docs_Dropdown
  - Find docs button (#docs-btn), click it
  - Verify docs menu (#docs-menu) is visible
  - Click outside (on editor area)
  - Verify docs menu closes

Admin/Editor/Graph_Strip
  - Verify graph strip (#graph-strip) exists
  - Click graph strip header (#graph-strip-header)
  - Verify graph strip body visibility toggles

Admin/Editor/Edit_Prefills_Data
  - Navigate to edit existing pipeline
  - Execute JS to get editor content: window._pikoEditor.state.doc.toString()
  - Verify content contains expected HCL (e.g., "my_cron_edit")
  - Verify name field (#name) has existing pipeline name
```

#### Priority 6: Pipeline Pause/Unpause

```
Admin/Pipeline/Pause_Unpause
  - Navigate to pipeline show page
  - Find pause button (#pause-pipeline), click it
  - Verify unpause button (#unpause-pipeline) appears (replaces pause)
  - Click unpause
  - Verify pause button returns
```

#### Priority 7: Dark Mode

```
Admin/Dark_Mode_Toggle
  - Click navbar dropdown
  - Click theme toggle (the .piko-toggle inside dropdown)
  - Execute JS: document.documentElement.getAttribute('data-theme')
  - Verify returns "dark"
  - Reload page (wd.Get(currentURL))
  - Execute JS again, verify still "dark"
  - Click dropdown again, click toggle to revert
  - Verify data-theme is removed/empty
```

#### Priority 8: Navigation and Access Control Edge Cases

```
Navigation/Not_Found_Redirect
  - Navigate to /this-route-does-not-exist
  - Verify redirected to teams page (breadcrumb "Teams")

Navigation/Direct_Deep_URL
  - Navigate directly to /teams/main/pipelines/cron (without clicking through UI)
  - Verify page loads correctly (graph SVG, breadcrumb)

Member/Cannot_Pause_Pipeline
  - As member, navigate to pipeline show
  - Verify #pause-pipeline and #unpause-pipeline do NOT exist

Member/Profile_Page
  - As member, click navbar dropdown, click Profile (#nav-profile)
  - Verify h1 "Profile"
  - Verify full_name and username fields are present

Public/No_Pipeline_Actions
  - As unauthenticated user viewing public pipeline
  - Verify #pause-pipeline does NOT exist
  - Verify #unpause-pipeline does NOT exist
  - Verify #edit-pipeline does NOT exist
  - Verify #delete-pipeline does NOT exist
```

#### Priority 9: Gear Panel Functional Verification

```
Admin/Gear_Panel/Hide_Intermediates_Effect
  - Open gear panel, note current graph node count
  - Check hide intermediates checkbox (#gear-hide-intermediates)
  - Wait for graph to re-render (SVG updates)
  - Verify graph node count or structure changed
  - Uncheck, verify graph returns to original
```

#### Priority 10: Resources Panel Functional Verification

```
Admin/Resources_Panel/Check_Resource
  - Open resources panel
  - Find check-resource-now button, click it
  - Wait for new version to appear (resource card status updates)
  - Verify resource card version info updates

Admin/Resources_Panel/Click_Resource_Link
  - Open resources panel
  - Click resource name link (.piko-resource-card-name)
  - Verify navigation to resource versions page (breadcrumb check)
```

### Tests NOT Added (too complex for Selenium or minimal risk)

- **Graph zoom/pan/fullscreen**: Mouse wheel/drag on SVG is unreliable in Selenium
- **Auto-scroll/follow mode in build logs**: Scroll position detection is flaky
- **Elapsed timer updates**: Timing-dependent, flaky in CI
- **Expanded step state preserved across re-renders**: Requires precise timing with polling
- **Resource versions scroll pagination**: Requires many versions, complex setup
- **Worker health banner**: Requires all workers to be down/stale
- **Worker version mismatch tooltips**: Bootstrap tooltip testing is unreliable
- **Worker delete stale**: Workers register via gRPC not HTTP; test worker is always healthy, can't create stale state
- **Local editor**: Requires separate test server setup (different entry point)
- **CodeMirror theme switching**: Internal editor state, not externally verifiable
- **Editor graph fullscreen (PikoGraphZoom overlay)**: Graph-specific fullscreen (separate from editor fullscreen) requires SVG overlay interaction, unreliable in headless
- **Resource error logs alert**: Requires a failed resource check to trigger, can't easily set up in test environment
- **Users profile edit (voluntary, non-forced)**: Profile page edit + token refresh chain is complex; forced password change already tested in login flow
- **Pipeline list view version selector**: Complex multi-step setup with specific data
- **Pipeline list view parallel group collapse**: Requires pipeline with parallel jobs
- **Build ID deep linking** (`/builds/:bid`): Requires knowing build IDs ahead of time
- **`?version=` URL param survives reload**: Marginal value for migration validation
- **Copy-to-clipboard verification**: Selenium cannot read clipboard reliably
- **Loading spinner visibility**: Too timing-sensitive for reliable testing
- **Form validation (empty fields)**: Server-side validation may differ, low migration risk
- **Toast messages for all CRUD ops**: Would make tests brittle/slow for marginal value

## CSS Impact

**None.** All existing CSS classes and selectors work as-is. Preact renders the same HTML elements with the same class names. The PICO-8 palette, Bootstrap overrides, dark mode — all unchanged.

## Migration Strategy

**Full rewrite** — not incremental. The codebase is ~5,500 lines of JS and maps cleanly to Preact components.

### Order of Work

0. **Add missing Selenium tests**: Add Priority 1-6 tests from the "Pre-Migration: Selenium Test Gap Analysis" section. Run all tests, verify they pass on the current Backbone codebase. Fix any pre-existing failures (e.g., `Admin/Resources_Panel` type text issue). This establishes the baseline.
1. **Vendor libraries**: Download Preact, HTM, preact-router, @preact/signals as ES module files into `/js/vendor/`
2. **Core infrastructure**: `api.js`, `state.js`, `utils.js`, `hooks.js`
3. **Layout shell**: `Layout.js` (Header, Notice/Toast, Breadcrumb) + `app.js` (Router setup)
4. **Auth flow**: `Login.js` + auth hooks + `Profile.js` (in `Users.js`)
5. **Teams**: `Teams.js` (list, create, show with member management)
6. **Pipeline list**: `PipelineList.js` (card list with live status polling)
7. **Pipeline show**: `PipelineShow.js` + `PipelineGraph.js` + `PipelineListView.js` (graph/list view, resources panel, gear panel, version tracking, job tree with chain resolution)
8. **Editor**: `Editor.js` (CodeMirror integration, HCL editing, blocks panel)
9. **Jobs**: `Jobs.js` (build history, logs, trigger/cancel/retry, polling)
10. **Resources**: `Resources.js` (versions, pin/unpin, trigger, webhook token)
11. **Admin**: `Users.js`, `Workers.js`
12. **Local editor**: `local-app.js` (standalone editor mode for `pikoci local`)
13. **Cleanup**: Remove Backbone/jQuery/Underscore files, update `index.tmpl` with import map
14. **Test all routes and interactions**

### Verification Checklist

Every item must pass before the migration is considered complete:

- [ ] Login/logout flow + `must_change_password` redirect to profile
- [ ] Each route accessible (authenticated routes redirect to login, public routes work without auth)
- [ ] Teams: list, create, show, edit, delete, member add/remove/admin toggle
- [ ] Pipeline graph renders correctly, zoom/pan/fullscreen all work
- [ ] Pipeline list view: job tree, resource selector, version selector, parallel group collapse/expand
- [ ] Pipeline resources panel: expand versions, check/trigger/pin inline
- [ ] Pipeline gear panel: hide intermediates + group parallel toggle re-renders graph
- [ ] Pipeline share panel: SVG/PNG/Markdown URLs, copy to clipboard
- [ ] Pipeline pause/unpause/delete with confirmation
- [ ] Editor: create new pipeline, edit existing, graph preview with 500ms debounce, blocks panel
- [ ] Editor: CodeMirror theme switches live when toggling dark mode
- [ ] Editor: clicking graph node jumps to block in editor
- [ ] Editor: HCL error diagnostics display inline
- [ ] Editor: vars.json tab works
- [ ] Editor: fullscreen mode (editor + graph) with Escape to close
- [ ] Build logs: steps expand/collapse, in_parallel groups render correctly
- [ ] Build logs: auto-scroll/follow mode, manual scroll disables follow, goto-bottom button
- [ ] Build logs: elapsed timer updates every 1s for running builds
- [ ] Build logs: cancel/retry buttons work, copy logs to clipboard
- [ ] Build logs: expanded step state preserved across polling re-renders
- [ ] Version tracking: end-to-end from resource version click → pipeline graph highlighting → filtered builds
- [ ] Version tracking: `?version=` URL param survives page reload and is shareable
- [ ] Resources page: trigger, pin/unpin, webhook panel (copy + regenerate), error logs alert
- [ ] Resource versions: scroll pagination (load more on scroll to bottom)
- [ ] Users: list, create, edit, delete with confirmation, reset password
- [ ] Profile: edit, change password, forced password change flow
- [ ] Workers: list with version mismatch banner, tooltips, delete stale workers
- [ ] Worker health banner in header for admin users
- [ ] Local editor: load config, edit, save, graph preview, toast notifications
- [ ] Dark mode toggle persists across reload (no FOUC)
- [ ] Live status polling works on pipeline cards, pipeline show, job builds, resource versions
- [ ] Toast notifications: success (4s auto-dismiss), error (8s auto-dismiss), manual dismiss
- [ ] Breadcrumb navigation works on all pages
- [ ] Not-found routes redirect to home
- [ ] Token refresh works (X-Refresh-Token header)

### Risk Mitigation

- The old frontend stays in git history — easy to revert if needed
- CSS is unchanged — visual regression risk is low
- REST API is unchanged — no backend coordination needed
- Each component can be tested in isolation before integrating
- Import maps are supported in all target browsers (all evergreen browsers since early 2023)

## File Count Comparison

| | Before | After |
|---|--------|-------|
| JS app files | 15 | ~19 (app.js, local-app.js, api.js, state.js, utils.js, hooks.js, graph-zoom.js, + 12 component files) |
| Vendored libs | 6 (jQuery, Underscore, Backbone, Bootstrap, Viz, CM) | 9 (Preact, Hooks, HTM, Router, Router-Match, Signals, Bootstrap, Viz, CM) |
| Template scripts in HTML | 30 | 0 |
| Total JS lines (est.) | ~5,500 | ~5,000-6,000 |
