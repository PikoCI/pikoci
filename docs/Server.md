# Server Configuration

Start the PikoCI server with:

```bash
pikoci server [flags]
```

## Flags

| Flag | Alias | Default | Required | Description |
|------|-------|---------|----------|-------------|
| `--port` | `-p` | `8080` | no | HTTP port |
| `--jwt-secret` | | | **yes** | Secret used to sign JWT tokens |
| `--users` | | | no | List of `USERNAME:HASHED_PASSWORD` pairs |
| `--db-system` | | `mem` | no | Database backend: `mem`, `sqlite`, `mysql`, `postgresql` |
| `--db-host` | | | no | Database host |
| `--db-port` | | | no | Database port |
| `--db-user` | | | no | Database user |
| `--db-password` | | | no | Database password |
| `--db-name` | | | no | Database name |
| `--run-migrations` | | `true` | no | Run database migrations on startup |
| `--run-worker` | | `true` | no | Run an embedded worker |
| `--concurrency` | | `1` | no | Number of worker goroutines |
| `--drain-timeout` | | `10m` | no | Max time to wait for in-flight jobs during graceful shutdown (`SIGQUIT`) |
| `--log-level` | | `info` | no | Log level: `debug`, `info`, `warn`, `error` |
| `--config` | `-c` | | no | Path to a config file |
| `--team-canonical` | | `main` | no | Team to use for `--pipeline-*` flags |
| `--pipeline-config` | | | no | Load a pipeline config file at startup |
| `--vars` | `-v` | | no | Path to a JSON vars file for the startup pipeline |
| `--pipeline-name` | `-n` | | no | Name for the startup pipeline |

## Environment variables

All flags can be set via environment variables. Use the flag name in uppercase with hyphens replaced by underscores:

```
<FLAG_NAME_UPPERCASED_WITH_UNDERSCORES>
```

Examples:

```bash
export PORT=9090
export JWT_SECRET=my-secret
export DB_SYSTEM=sqlite
```

## Default user

The initial database migration seeds a default user: `admin` / `admin123`. On first login with the default password, the UI will redirect to the Profile page and require a password change before continuing. Use the `--users` flag to create users or override the default password at startup.

```bash
# Override the default admin password
./pikoci user-password -u admin -p new-secure-password
# Output: admin:$2a$10$...

./pikoci server --jwt-secret my-secret --users 'admin:$2a$10$...'

# Add a new user
./pikoci user-password -u deploy -p s3cret
# Output: deploy:$2a$10$...

./pikoci server --jwt-secret my-secret --users 'admin:$2a$10$...' --users 'deploy:$2a$10$...'
```

The `--users` flag is safe to pass on every restart. It will set the password for the `admin` user if it still has the default `admin` / `admin123` credentials, but will not overwrite passwords that have been changed via the UI or CLI.

## Examples

### In-memory (development)

```bash
pikoci server --jwt-secret dev-secret --db-system mem
```

### SQLite (single node)

```bash
pikoci server --jwt-secret prod-secret --db-system sqlite --db-name pikoci.db
```

### PostgreSQL (production)

```bash
pikoci server \
  --jwt-secret prod-secret \
  --db-system postgresql \
  --db-host db.example.com \
  --db-port 5432 \
  --db-user pikoci \
  --db-password secret \
  --db-name pikoci \
  --run-worker=false
```

When `--run-worker=false`, the server logs a pre-generated worker token on startup. Copy this token to configure standalone workers with `--worker-token`. See [Running Workers Separately](Workers.md).

### Load a pipeline at startup

```bash
pikoci server \
  --jwt-secret my-secret \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl \
  --vars vars.json
```

## Horizontal scaling

PikoCI supports running multiple server instances concurrently when using PostgreSQL or MySQL as the database backend. The scheduler uses `SELECT ... FOR UPDATE SKIP LOCKED` to ensure each resource check is processed by only one instance.

SQLite and in-memory backends are single-instance only (no locking support).

## Metrics

PikoCI exposes a `/metrics` endpoint in Prometheus format with Go runtime metrics (goroutines, memory, GC), HTTP request counts by status code and method, and HTTP request duration histograms.

```bash
curl http://localhost:8080/metrics
```

See [Deployment — Monitoring](Deployment.md#monitoring) for setting up Prometheus and Grafana.

## Signal handling

PikoCI supports two shutdown modes:

| Signal | Behavior |
|--------|----------|
| `SIGQUIT` | **Graceful shutdown.** Stops accepting new jobs, waits for in-flight jobs to finish (up to `--drain-timeout`, default 10m), then gracefully shuts down the HTTP server. |
| `SIGTERM` / `SIGINT` | **Immediate shutdown.** Cancels all running jobs and exits. |

Graceful shutdown (`SIGQUIT`) is designed for zero-downtime self-deploys: a pipeline job builds the new binary, copies it, and sends `SIGQUIT`. The running job finishes, PikoCI exits cleanly, and systemd restarts with the new binary.

When `--run-worker=false` (external workers), the server shuts down the HTTP server immediately on `SIGQUIT` since workers are separate processes.

```bash
# Graceful shutdown (finish running jobs first)
kill -QUIT $(pidof pikoci)

# Immediate shutdown
kill -TERM $(pidof pikoci)
```

See also: [Database Backends](Database.md) · [Running Workers Separately](Workers.md)
