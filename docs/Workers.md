# Running Workers Separately

By default, PikoCI runs an embedded worker inside the server process. For production setups, you can run workers as separate processes on different machines.

## Architecture

Standalone workers connect to the server using gRPC bidirectional streaming. The server pushes jobs to workers as they become available, and workers stream results back in real time. Data queries (GetPipeline, UpdateJobBuild, etc.) use standard HTTP. Both gRPC and HTTP are multiplexed on the same port via cmux.

```
                    ┌─────────┐
                    │  Server  │  --run-worker=false
                    └────┬────┘
                         │ gRPC streaming (same port)
              ┌──────────┼──────────┐
              │          │          │
         ┌────┴───┐  ┌───┴────┐  ┌─┴──────┐
         │Worker 1│  │Worker 2│  │Worker 3│
         └────────┘  └────────┘  └────────┘
```

**Key benefits of gRPC streaming:**
- **Instant cancellation** — server pushes `CancelJob` to the worker (no polling delay)
- **Built-in keepalive** — heartbeats flow over the stream
- **Server-push dispatch** — no wasted polling cycles
- **Typed contracts** — protobuf schema (`proto/worker/v1/worker.proto`)

The embedded worker (when `--run-worker=true`) calls the service directly in-process — no gRPC or HTTP involved.

## Requirements

- Workers must be able to reach the server URL.
- Workers need a worker token for authentication. Generate one with `pikoci worker-token --jwt-secret <secret>` or copy it from the server startup logs.

## Server setup

Disable the embedded worker. The server logs a worker token on startup that you can copy for worker config:

```bash
pikoci server \
  --jwt-secret my-secret \
  --db-system postgresql \
  --db-host db.example.com \
  --db-port 5432 \
  --db-user pikoci \
  --db-password secret \
  --db-name pikoci \
  --run-worker=false
# Server logs: Worker token for standalone workers token=eyJhbG...
```

Or generate a token manually:

```bash
pikoci worker-token --jwt-secret my-secret
# Output: eyJhbG...
```

## Worker setup

```bash
pikoci worker \
  --pikoci-url http://server:8080 \
  --worker-token eyJhbG... \
  --concurrency 4
```

## Worker flags

| Flag | Alias | Default | Required | Description |
|------|-------|---------|----------|-------------|
| `--pikoci-url` | `-u` | `localhost:8080` | no | PikoCI server URL |
| `--concurrency` | | `1` | no | Number of parallel job goroutines |
| `--tags` | | | no | Comma-separated worker tags for job/resource routing (e.g. `gpu,vpn`) |
| `--exclusive-tags` | | `false` | no | When set, worker only handles work matching its tags (skips untagged jobs/resources) |
| `--drain-timeout` | | `10m` | no | Max time to wait for in-flight jobs during graceful shutdown (`SIGQUIT`) |
| `--log-level` | | `info` | no | Log level: `debug`, `info`, `warn`, `error` |
| `--worker-token` | | | **yes** | Worker authentication token (from `pikoci worker-token` or server startup logs) |
| `--config` | `-c` | | no | Path to a config file |

## Environment variables

Worker flags can be set via environment variables:

```bash
export PIKOCI_URL=http://server:8080
export WORKER_TOKEN=eyJhbG...
export CONCURRENCY=4
export TAGS=gpu,vpn
export EXCLUSIVE_TAGS=true
```

## Worker names

Each worker registers with a unique name. By default, a random name is generated automatically. You can set a custom name with `--name`:

```bash
pikoci worker --name build-machine-1 ...
```

Worker names must be unique across all running workers. If two workers use the same `--name`, they will overwrite each other's heartbeat in the database and appear as a single worker in the dashboard. This can be useful for rolling deploys (stop the old worker, start a new one with the same name), but running two workers simultaneously with the same name will cause incorrect status reporting.

When `--concurrency` is greater than 1, each goroutine within the process automatically gets a `-N` suffix (e.g. `build-machine-1-1`, `build-machine-1-2`).

## Tags

Tags route specific jobs and resource checks to specific workers. A job with `tags = ["gpu"]` in its pipeline definition will only be dispatched to workers started with `--tags gpu`.

### Matching rules

- **AND logic**: a job with `tags = ["gpu", "vpn"]` requires a worker with **both** tags.
- **Untagged jobs** run on any non-exclusive worker (including tagged workers).
- **Tagged jobs** only run on workers that have all of the job's tags.
- Tags on resources work the same way — a resource with `tags = ["vpn"]` will only have its checks run on workers with the `vpn` tag.

### Exclusive mode

By default, a tagged worker handles both tagged work matching its tags AND untagged work. Adding `--exclusive-tags` restricts the worker to only handle work that matches its tags:

```bash
# Handles gpu-tagged jobs AND untagged jobs
pikoci worker --tags gpu --worker-token eyJhbG...

# Handles ONLY gpu-tagged jobs (ignores untagged jobs)
pikoci worker --tags gpu --exclusive-tags --worker-token eyJhbG...
```

### Example setup

```hcl
# Pipeline: gpu-job only runs on gpu workers, deploy-job on deploy workers
job "build" {
  task "compile" { run "exec" { path = "make" } }
}

job "train-model" {
  tags = ["gpu"]
  task "train" { run "exec" { path = "./train.sh" } }
}

job "deploy" {
  tags = ["deploy"]
  task "release" { run "exec" { path = "./deploy.sh" } }
}
```

```bash
# General worker — handles "build" (untagged)
pikoci worker --worker-token eyJhbG...

# GPU worker — handles "build" + "train-model"
pikoci worker --tags gpu --worker-token eyJhbG...

# Deploy worker (exclusive) — handles ONLY "deploy"
pikoci worker --tags deploy --exclusive-tags --worker-token eyJhbG...
```

### Tag matching reference

| Job tags | Worker tags | Worker exclusive | Match? |
|----------|------------|-----------------|--------|
| none | none | no | Yes |
| none | `["gpu"]` | no | Yes |
| none | `["gpu"]` | yes | No |
| `["gpu"]` | none | no | No |
| `["gpu"]` | `["gpu"]` | no | Yes |
| `["gpu"]` | `["gpu"]` | yes | Yes |
| `["gpu"]` | `["gpu", "vpn"]` | no | Yes |
| `["gpu", "vpn"]` | `["gpu"]` | no | No |

### Validation

Tags must be valid slugs: lowercase letters, digits, and hyphens. Maximum 10 tags per job, resource, or worker. Invalid tags are rejected at pipeline parse time (for jobs/resources) or worker startup (for workers).

Tags are visible in the workers dashboard alongside the worker status and platform information.

## Team-scoped workers

For multi-tenant environments, teams can generate dedicated worker tokens that restrict workers to only process that team's builds and resource checks. This provides a hard security boundary — team workers never access other teams' secrets or source code.

### Generating a team worker token

Team admins can generate a token from the web UI (**Team Settings > Workers** tab) or via the CLI:

```bash
pikoci client teams worker-token --team-canonical my-team --url http://server:8080
# Output: eyJhbG...
```

### Starting a team-scoped worker

```bash
pikoci worker \
  --pikoci-url http://server:8080 \
  --worker-token <team-token> \
  --concurrency 2
```

### Dispatch behavior

- **Team worker**: only receives builds and resource checks from its team.
- **Global worker**: serves teams that have no dedicated team workers. When a team has at least one online team worker, global workers skip that team's work.
- **Tags compose**: a team worker with `--tags gpu` only gets GPU-tagged jobs from its team.

### Token regeneration

Regenerating a team's worker token invalidates the previous token. Workers using the old token will fail to re-register and must be restarted with the new token.

### Workers dashboard

The global workers dashboard shows a **Team** column for each worker, displaying the team canonical name or "Global" for non-team workers.

## Reverse proxy configuration

Since gRPC and HTTP share the same port, your reverse proxy needs to handle HTTP/2 for gRPC. Most proxies support this automatically.

**Caddy** (works out of the box — Caddy handles HTTP/2 automatically):
```
ci.example.com {
    reverse_proxy localhost:8080
}
```

**nginx** (requires HTTP/2 backend support):
```nginx
server {
    listen 443 ssl http2;
    server_name ci.example.com;

    location / {
        grpc_pass grpc://localhost:8080;
        proxy_pass http://localhost:8080;
    }
}
```

## Scaling

Run multiple worker instances to increase throughput. Each worker connects via gRPC and receives jobs as they become available:

```bash
# Machine A
pikoci worker --pikoci-url http://server:8080 --worker-token eyJhbG... --concurrency 2

# Machine B
pikoci worker --pikoci-url http://server:8080 --worker-token eyJhbG... --concurrency 4

# Machine C
pikoci worker --pikoci-url http://server:8080 --worker-token eyJhbG...
```

## Monitoring workers

Workers send periodic heartbeats to the server. The server tracks each worker's name, platform, version, queues, and last heartbeat time.

### Workers dashboard

Admin users can view all registered workers at **`#workers`** in the web UI. Each worker shows:

- **Status**: `healthy` (heartbeat received within the last 90 seconds) or `stale` (no heartbeat for over 90 seconds)
- **Tags**: the worker's tags and whether it's in exclusive mode
- **Platform**: OS, architecture, and Go version
- **Version**: the PikoCI binary version
- **Uptime**: how long the worker has been running
- **Last Seen**: when the last heartbeat was received

### Health banner

When no healthy workers are detected, admin users see a warning banner at the top of every page: "No healthy workers detected. Builds will queue until a worker comes online." This banner links to the workers dashboard.

### Health API

The `GET /workers/health` endpoint returns whether at least one worker is healthy. This is useful for external monitoring:

```bash
curl -H "Authorization: Bearer $TOKEN" http://server:8080/workers/health
# {"healthy":true}
```

### Prometheus metrics

The server exposes a `pikoci_workers` gauge on the `/metrics` endpoint with a `status` label (`healthy` or `stale`). This updates every 30 seconds.

```
# HELP pikoci_workers Number of registered workers by status.
# TYPE pikoci_workers gauge
pikoci_workers{status="healthy"} 2
pikoci_workers{status="stale"} 1
```

Example Prometheus alert for zero healthy workers:

```yaml
- alert: NoHealthyWorkers
  expr: pikoci_workers{status="healthy"} == 0
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "No healthy PikoCI workers"
```

### Stale workers

A worker becomes **stale** when the server has not received a heartbeat from it for more than 90 seconds. This typically means the worker process was stopped, crashed, or lost network connectivity.

Stale workers remain visible in the dashboard. They are not removed automatically, so admins can see what was expected to be running and investigate why it stopped. The worker itself is not notified or affected when it becomes stale; the status is computed server-side based on the last heartbeat time.

### Deleting workers

Admins can delete stale workers from the dashboard by clicking the trash icon button. This only removes the worker's registration from the database. It does not send any signal to the worker process itself. If the worker is still running (e.g. it recovered from a network issue), it will re-register on its next heartbeat.

The delete endpoint is also available via the API:

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://server:8080/workers/worker-name
```

## Signal handling

Standalone workers support the same two shutdown modes as the server:

| Signal | Behavior |
|--------|----------|
| `SIGQUIT` | Stop accepting new jobs, wait for in-flight jobs to finish (up to `--drain-timeout`, default 10m), then exit. |
| `SIGTERM` / `SIGINT` | Cancel running jobs and exit immediately. |

```bash
# Graceful shutdown
kill -QUIT $(pidof pikoci)

# Immediate shutdown
kill -TERM $(pidof pikoci)
```
