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
