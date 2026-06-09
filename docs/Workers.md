# Running Workers Separately

By default, PikoCI runs an embedded worker inside the server process. For production setups, you can run workers as separate processes on different machines.

## Architecture

PikoCI uses two separate queues: one for **jobs** and one for **resource checks**. This prevents long-running jobs (e.g. Docker builds) from blocking resource check processing.

```
                    ┌─────────┐
                    │  Server  │  --run-worker=false
                    └────┬────┘
                    ┌────┴────┐
              ┌─────┤  Queues ├─────┐
              │     └─────────┘     │
         jobs │                     │ checks
              │                     │
         ┌────┴───┐           ┌────┴───┐
         │Worker 1│           │Worker 2│
         │(all)   │           │(checks)│
         └────────┘           └────────┘
```

The server publishes jobs and resource checks to separate queues. Workers subscribe, execute the work, and report results back via the PikoCI API.

## Requirements

- A non-memory queue backend (`nats`, `rabbit`, or `kafka`). The `mem` backend only works within a single process.
- Workers must be able to reach the server URL and the queue backend.
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
  --pubsub-system nats \
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
  --pubsub-system nats \
  --worker-token eyJhbG... \
  --concurrency 4
```

## Worker flags

| Flag | Alias | Default | Required | Description |
|------|-------|---------|----------|-------------|
| `--pikoci-url` | `-u` | `localhost:8080` | no | PikoCI server URL |
| `--pubsub-system` | | `mem` | no | Queue backend (must match server) |
| `--queues` | | `jobs,checks` | no | Which queues to listen on: `jobs`, `checks`, or `jobs,checks` |
| `--concurrency` | | `1` | no | Number of parallel job goroutines |
| `--drain-timeout` | | `10m` | no | Max time to wait for in-flight jobs during graceful shutdown (`SIGQUIT`) |
| `--log-level` | | `info` | no | Log level: `debug`, `info`, `warn`, `error` |
| `--worker-token` | | | **yes** | Worker authentication token (from `pikoci worker-token` or server startup logs) |
| `--config` | `-c` | | no | Path to a config file |

## Environment variables

Worker flags can be set via environment variables:

```bash
export PIKOCI_URL=http://server:8080
export PUBSUB_SYSTEM=nats
export WORKER_TOKEN=eyJhbG...
export CONCURRENCY=4
```

## Worker names

Each worker registers with a unique name. By default, a random name is generated automatically. You can set a custom name with `--name`:

```bash
pikoci worker --name build-machine-1 ...
```

Worker names must be unique across all running workers. If two workers use the same `--name`, they will overwrite each other's heartbeat in the database and appear as a single worker in the dashboard. This can be useful for rolling deploys (stop the old worker, start a new one with the same name), but running two workers simultaneously with the same name will cause incorrect status reporting.

When `--concurrency` is greater than 1, each goroutine within the process automatically gets a `-N` suffix (e.g. `build-machine-1-1`, `build-machine-1-2`).

## Scaling

Run multiple worker instances to increase throughput. Each worker independently subscribes to the queues:

```bash
# Machine A — handles both jobs and checks (default)
pikoci worker --pikoci-url http://server:8080 --pubsub-system nats --worker-token eyJhbG... --concurrency 2

# Machine B — dedicated job runner
pikoci worker --pikoci-url http://server:8080 --pubsub-system nats --worker-token eyJhbG... --concurrency 4 --queues jobs

# Machine C — dedicated check worker (never blocked by long jobs)
pikoci worker --pikoci-url http://server:8080 --pubsub-system nats --worker-token eyJhbG... --queues checks
```

## Dedicated check workers

When jobs take minutes (e.g. Docker builds), a single worker processing both queues will block resource checks until the job finishes. Use `--queues checks` to run a dedicated check worker that detects new versions promptly, regardless of job load.

## Monitoring workers

Workers send periodic heartbeats to the server. The server tracks each worker's name, platform, version, queues, and last heartbeat time.

### Workers dashboard

Admin users can view all registered workers at **`#workers`** in the web UI. Each worker shows:

- **Status**: `healthy` (heartbeat received within the last 90 seconds) or `stale` (no heartbeat for over 90 seconds)
- **Queues**: which queues the worker listens on (`jobs`, `checks`, or both)
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
