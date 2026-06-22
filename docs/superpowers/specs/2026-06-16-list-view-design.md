# List View + View Switcher Design Spec

**Issue:** #538 (part of #533)
**Date:** 2026-06-16

## Problem

The pipeline show page only offers a graph view. Users who want a quick "did my build pass?" check have to parse a full DAG. A simpler list view showing jobs and their status would serve this use case better.

## Scope

- View switcher toolbar: [Graph] [Pipeline (disabled)] [List]
- Trigger bar showing the resource version that triggered the latest build
- List view with left panel (job list) and right panel (job detail with steps/logs)
- Parallel jobs nested under group headers (matching `in_parallel` plan structure)
- View preference persisted in localStorage

## Architecture

### 1. Backend: New `ListPipelineJobs` endpoint

**Service interface** (`pikoci/service.go`):
```go
ListPipelineJobs(ctx context.Context, tc, pn string) ([]JobWithStatus, error)
ListPublicPipelineJobs(ctx context.Context, tc, pn string) ([]JobWithStatus, error)
```

**JobWithStatus** (new type in `pikoci/job/job.go`):
```go
type JobWithStatus struct {
    Job
    LatestStatus       string    `json:"latest_status"`       // succeeded, failed, started, pending, cancelled, ""
    HasRunning         bool      `json:"has_running"`
    LatestBuildNumber  string    `json:"latest_build_number"`
    LatestBuildDuration int64    `json:"latest_build_duration"` // nanoseconds
    StartedAt          *time.Time `json:"started_at,omitempty"`
}
```

**Implementation** (`pikoci/pipelines.go`): Reuse the same `Builds.Filter` pattern from `generateImage` — for each job, find the latest completed build and check for running/pending builds.

**HTTP handler** (`pikoci/transport/http/jobs.go`):
- Route: `GET /teams/{team_canonical}/pipelines/{pipeline_canonical}/jobs`
- Route name: `ListPipelineJobs`
- Authorization: `member` (with public pipeline fallback via `IsPublicAccessKey`)
- Response: `{"data": [...]}`

### 2. Frontend: View Switcher

**Template** (`index.tmpl` — `#pipeline-show-view`):
- Add toolbar div with three buttons: Graph, Pipeline (disabled), List
- Add trigger bar div below toolbar
- Wrap existing graph container in a `piko-view-graph` div
- Add `piko-view-list` div (hidden by default)

**JavaScript** (`pipelines.js` — `PipelineShowView`):
- New events: `click .piko-view-btn`
- `switchView(mode)` method: toggles visibility of graph/list containers, updates active button, saves to localStorage key `piko-pipeline-view`
- On initialize: read localStorage, default to `"graph"`
- List view only initializes its sub-view when first switched to (lazy)

### 3. Frontend: List View

**New view** `PipelineListView` (in `pipelines.js`):

**Left panel — Job list:**
- Fetches from `Jobs` collection (already exists in `collections.js`, URL: `pipeline.url() + "/jobs"`)
- Renders jobs from the plan order, preserving `in_parallel` nesting
- Walking `pipeline.jobs[].plan` to detect `in_parallel` step types for grouping
- Each job row shows: status dot + name + status badge
- Parallel group header shows: ▼/▶ + "parallel" label + aggregate status counts
- Clicking a job selects it (adds `.active` class)
- Polls via `fetchInterval` for status updates

**Right panel — Job detail:**
- When a job is selected, fetches its latest build via existing `Builds` collection (`limit=1`)
- Renders build steps with status icons, duration, and log output
- Reuses the step rendering pattern from `JobBuildsContentView` template
- Auto-follow for running logs
- Polls running builds for live updates

**Trigger bar:**
- Shows the resource version that triggered the latest build
- Derived from the first `get` step's version in the most recent build
- Format: `latest build: [resource.name @ version_ref]`

### 4. CSS

New styles in `pikoci.css`:
- `.piko-view-toolbar` — flexbox toolbar matching mockup
- `.piko-view-btn` / `.piko-view-btn.active` / `.piko-view-btn.disabled`
- `.piko-trigger-bar` — trigger info strip
- `.piko-list-view` — flex container for left/right panels
- `.piko-job-list` — left panel (240px width)
- `.piko-job-row` / `.piko-job-row.active` — job list items
- `.piko-parallel-header` — collapsible group header
- `.piko-parallel-nested` — indented container
- `.piko-job-detail` — right panel (flex: 1)

### 5. Data Flow

```
User opens pipeline page
  → PipelineShowView reads localStorage("piko-pipeline-view")
  → If "list": initializes PipelineListView
     → Fetches GET /teams/{tc}/pipelines/{pc}/jobs
     → Renders job list in left panel
     → Auto-selects first non-succeeded job (or first job)
     → Fetches GET /teams/{tc}/pipelines/{pc}/jobs/{name}/builds?limit=1
     → Renders build steps in right panel
     → Polls both endpoints on fetchInterval
```

### 6. Files to Modify

**Backend:**
- `pikoci/job/job.go` — add `JobWithStatus` type
- `pikoci/service.go` — add `ListPipelineJobs`, `ListPublicPipelineJobs` to interface
- `pikoci/pipelines.go` — implement `ListPipelineJobs`, `ListPublicPipelineJobs`
- `pikoci/transport/http/jobs.go` — add `listPipelineJobs` handler
- `pikoci/transport/http/handler.go` — register route
- `pikoci/transport/http/route_names.go` — add `ListPipelineJobs`
- `pikoci/transport/http/authorization.go` — add `ListPipelineJobs: member`
- `pikoci/transport/http/client/client.go` — add client method
- `pikoci/mock/service.go` — regenerate
- `pikoci/transport/http/route_names_string.go` — regenerate

**Frontend:**
- `pikoci/transport/http/templates/views/layouts/index.tmpl` — update `#pipeline-show-view`, add list view templates
- `pikoci/transport/http/assets/js/app/views/pipelines.js` — view switcher logic, `PipelineListView`
- `pikoci/transport/http/assets/css/pikoci.css` — list view styles

**Tests:**
- `pikoci/pipelines_test.go` — test `ListPipelineJobs`
- `pikoci/transport/http/handlers_coverage_test.go` — add handler coverage
