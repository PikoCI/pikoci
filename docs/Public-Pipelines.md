---
description: "Share CI/CD pipeline status publicly. Let anyone view builds and job status without authentication."
---

# Public Pipelines

Public pipelines let anyone view a pipeline's status without authentication. Useful for open-source projects that want to display build status.

## Making a pipeline public

Use the `--public` flag when updating a pipeline:

```bash
pikoci client -u localhost:8080 pipelines update -n my-pipeline -c pipeline.hcl --public
```

## What is exposed

Public pipeline endpoints return a sanitized view of the pipeline. The following data is **included**:

- Pipeline name and structure (jobs, resources, resource types)
- Job names and plan steps
- Resource names and types
- Resource type names and IDs
- Build status

The following data is **removed** for security:

| Field | Reason |
|-------|--------|
| `raw` (pipeline config) | May contain secrets in variable values |
| Resource `params` | May contain URLs, credentials |
| Resource `webhook_token` | Would allow unauthorized triggers |
| Resource `logs` | May contain sensitive output |
| Resource `cron_id` | Internal implementation detail |
| Resource type `check`/`pull`/`push` commands | May contain secrets in args |
| Resource type `params` list | Only `id` and `name` are retained |

## Public API endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /teams/{team}/pipelines/{pipeline}/public` | Sanitized pipeline data |
| `GET /teams/{team}/pipelines/{pipeline}/image.dot` | Pipeline graph in DOT format (JSON response) |
| `GET /teams/{team}/pipelines/{pipeline}/image.svg` | Pipeline graph as SVG image |
| `GET /teams/{team}/pipelines/{pipeline}/image.png` | Pipeline graph as PNG image |

SVG and PNG endpoints return raw image data with the appropriate `Content-Type` header and `Access-Control-Allow-Origin: *` for cross-origin embedding. The pipeline must be public for unauthenticated access.

### Query parameters

The SVG, PNG, and DOT image endpoints accept optional query parameters to customize the rendered graph:

| Parameter | Value | Description |
|-----------|-------|-------------|
| `hide_intermediates` | `1` | Hide intermediate resource nodes between jobs, drawing direct job-to-job edges (keeps only entry-point trigger resources) |
| `group_parallel` | `1` | Collapse jobs sharing identical upstream parents into a single compact node |

Example:

```
/teams/main/pipelines/my-pipeline/image.svg?hide_intermediates=1&group_parallel=1
```

## Sharing pipeline graphs

In the graph view, click the **share button** (<i class="bi bi-share"></i>) next to the gear icon to open a panel with copyable URLs for the pipeline graph:

- **SVG**: direct link to the SVG image
- **PNG**: direct link to the PNG image
- **Markdown**: ready-to-paste `![pipeline](url)` snippet

The URLs automatically include query parameters matching your current gear options (hide intermediates, group parallel), so what you see is what gets shared.

## Embedding pipeline graphs

Public pipelines can be embedded directly in READMEs, dashboards, or any HTML page. **The pipeline must be marked as public** (see above) for unauthenticated embedding to work. Non-public pipelines require authentication and will return an error for anonymous requests.

**Markdown:**

```markdown
![Pipeline](https://ci.example.com/teams/main/pipelines/my-pipeline/image.svg)
```

**Markdown with options:**

```markdown
![Pipeline](https://ci.example.com/teams/main/pipelines/my-pipeline/image.svg?hide_intermediates=1&group_parallel=1)
```

**HTML:**

```html
<img src="https://ci.example.com/teams/main/pipelines/my-pipeline/image.svg" alt="Pipeline status" />
```

## Example

```bash
# View public pipeline data
curl http://localhost:8080/teams/main/pipelines/my-pipeline/public

# Get the pipeline graph as SVG
curl http://localhost:8080/teams/main/pipelines/my-pipeline/image.svg > status.svg

# Get as PNG
curl http://localhost:8080/teams/main/pipelines/my-pipeline/image.png > status.png

# Get as DOT (JSON response, for piping to graphviz locally)
curl http://localhost:8080/teams/main/pipelines/my-pipeline/image.dot
```
