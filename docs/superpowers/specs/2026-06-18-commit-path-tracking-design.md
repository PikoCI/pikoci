# Commit Path Tracking: Where Is My Version?

**Issue**: #432
**Date**: 2026-06-18

## Problem

There is no way to see what steps a specific commit/version will go through, which have completed, and what's next. Users manually check multiple jobs and pipelines to answer "is my commit deployed?"

## Solution: Version-Scoped Pipeline Views

When a user selects a specific resource version, both the list view and graph view scope to show only the jobs in that version's path, with their specific builds.

### Terminology

- **Track**: The user-facing verb/action. "Track version abc123" scopes the pipeline view to that version.
- **Version path**: The ordered set of jobs a version passes through, derived from the pipeline's `passed` constraints.
- **Version scope**: The active filter state when tracking a version.

### Entry Points (3 ways to enter tracking mode)

All use the **"Track"** label with the `bi-signpost-2` Bootstrap icon.

1. **Resource Versions Page** — "Track" button on each version row, alongside existing Trigger and Pin actions
2. **Resources Panel** — expandable version list per resource, each version has Track / Trigger / Pin actions
3. **List View Version Dropdown** — a second dropdown next to the existing resource selector, showing recent versions with aggregate status dots. Selecting a version scopes the view.

### Version Scope Banner

A persistent banner appears between the view toolbar (Graph|List|Resources|Gear|Share) and the content area when tracking is active. It shows:

- "Tracking version" label
- Resource name and version identifier
- Progress summary (e.g., "2/4 completed")
- "Clear" button to exit tracking mode

The banner appears in both graph and list views.

### List View (Version-Scoped)

When a version is selected:

- Job list shows only jobs in the version's path (same chain resolution as current resource scoping via `_resolveChain()`)
- Build detail panel shows the specific build triggered by this version
- Retries are accessible from the build detail
- Version dropdown shows "abc123" instead of "All"
- Clear button removes version scope, returning to normal resource-scoped view

### Graph View (Version-Scoped)

When a version is selected:

- Graph is **regenerated server-side** via GraphViz with only the version's path jobs (same approach as existing `hideIntermediates` and `groupParallel` filters)
- Job node labels include build number, status, and duration
- Running jobs use dashed borders (existing pattern)
- Scope banner appears above the graph

### Backend API

**New endpoint**: `GET /api/v1/teams/:tc/pipelines/:pn/resources/:rCan/versions/:vID/path`

**Chain resolution** (server-side BFS):
1. Find jobs that consume the resource with no `passed` constraints (entry-point jobs)
2. BFS through downstream jobs via `passed` constraints
3. Returns ordered chain of jobs

**Build lookup** (per job in chain):
- Query `build_get_versions` table to find builds that consumed the specific version ID
- Include retries via `RetrySourceBuildID`

**Response shape**:
```json
{
  "resource": { "canonical": "git.repo", "version": {"ref": "abc123"} },
  "path": [
    {
      "job_name": "build",
      "build": { "id": 42, "build_number": "42", "status": "succeeded", "duration": 83 },
      "retries": []
    },
    {
      "job_name": "deploy-prod",
      "build": null
    }
  ]
}
```

**Graph regeneration**: Add a `version_id` query parameter to the existing pipeline image endpoint. When present, `generateImage()` restricts the graph to only the jobs in the version's path and annotates nodes with build info.

### URL Scheme

Version tracking state is reflected in the URL for shareability:
- Graph view: `teams/:tc/pipelines/:pn?version=:vID`
- List view: `teams/:tc/pipelines/:pn/jobs/:jn/builds?version=:vID`

### Polling

When in version-scoped mode:
- Poll the version path endpoint at the standard `fetchInterval` (30s)
- Update job statuses and build info in real-time
- Automatically clear scope if the version no longer exists (edge case)

### What's NOT in Scope (v1)

- Cross-pipeline tracking via trigger resources (mentioned in issue as future extension)
- Notifications referencing path state (related to #277)
- Build log display within the version path view (use existing build detail page)

## Key Files

- **List view JS**: `pikoci/transport/http/assets/js/app/views/pipelines.js` (PipelineListView, `_resolveChain()`)
- **Graph generation**: `pikoci/pipelines.go` (`generateImage()`)
- **Build-version tracking**: `pikoci/mysql/build.go` (`build_get_versions` table, `AggregateStatusByVersionIDs()`)
- **Resource versions view**: `pikoci/transport/http/assets/js/app/views/resources.js`
- **Resources panel view**: `pikoci/transport/http/assets/js/app/views/pipelines.js` (PipelineResourcesPanelView)
- **HTTP handlers**: `pikoci/transport/http/resources.go`, `pikoci/transport/http/pipelines.go`
- **Templates**: `pikoci/transport/http/templates/views/layouts/index.tmpl`
- **CSS**: `pikoci/transport/http/assets/css/pikoci.css`
- **Router**: `pikoci/transport/http/assets/js/app/router.js`
