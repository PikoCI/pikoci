# Services

A service type defines an ephemeral process that runs alongside a job's tasks. Services are started before tasks and stopped unconditionally after, regardless of whether the tasks succeed or fail. Common use cases include databases, caches, message brokers, or any dependency that needs to be running while tasks execute.

## Defining a service type

Service types are runner-agnostic. You can start any process: a local daemon, a container, a background script, etc.

```hcl
service_type "postgres" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "pg_ctl -D $WORKDIR/pgdata -l $WORKDIR/pg.log start"]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "pg_isready"]
    interval = "1s"
    timeout  = "30s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "pg_ctl -D $WORKDIR/pgdata stop"]
  }
}
```

| Field         | Required | Description                                                      |
|---------------|----------|------------------------------------------------------------------|
| `name`        | yes      | Label on the block                                               |
| `source`      | no       | URL to fetch the definition from (mutually exclusive with inline `start`/`stop`) |
| `params`      | no       | List of parameter names for per-job customization                |
| `start`       | yes*     | Runner command to start the service                              |
| `ready_check` | no       | Runner command to verify the service is ready (polled)           |
| `stop`        | yes*     | Runner command to stop the service (always runs)                 |

\* Not required when `source` is set.

### start

The `start` block runs once when the job begins, before any `get`, `task`, or `put` steps. If start fails, the job fails immediately and `stop` is still called for any already-started services.

### ready_check

The `ready_check` block is optional. When present, PikoCI polls the command at the specified interval until it exits with code 0 (ready) or the timeout is exceeded (fail). If no `ready_check` is defined, the job proceeds immediately after `start` completes.

| Field      | Default | Description                            |
|------------|---------|----------------------------------------|
| `interval` | `"1s"`  | Time between ready check attempts      |
| `timeout`  | `"60s"` | Maximum time to wait for readiness     |

### stop

The `stop` block runs unconditionally after all tasks complete, whether they succeeded or failed. Stop failures are logged but do not change the job's status.

## Sourcing from URL

Instead of defining `start`/`stop` commands inline, you can point to an external HCL file:

```hcl
service_type "postgres" {
  source = "https://example.com/services/postgres.hcl"
  params = ["version"]
}
```

Two URL formats are supported:

- **`pikoci://<name>`** resolves to the PikoCI registry (no built-in services are shipped yet, but this is reserved for future additions).
- **`https://...`** or **`http://...`** fetches HCL from any URL.

When `source` is set, you must not define inline `start`, `stop`, or `ready_check` blocks. PikoCI will error if both are present.

> **Note:** The source is resolved once when the pipeline is created or updated. If the remote HCL file changes, you must re-set the pipeline to pick up the new definition.

## Referencing service types in jobs

Reference a top-level service type by name in a job:

```hcl
job "test" {
  service "postgres" {}

  get "cron" "timer" { trigger = true }
  task "run-tests" {
    run "exec" {
      path = "make"
      args = ["test"]
    }
  }
}
```

### Param overrides

Pass parameters to customize a service per job:

```hcl
job "test-pg15" {
  service "postgres" {
    version = "15"
  }
  ...
}

job "test-pg16" {
  service "postgres" {
    version = "16"
  }
  ...
}
```

Parameters are available in the service's start, ready_check, and stop commands as `$param_<name>`.

## Environment variables

Inside service commands (`start`, `ready_check`, `stop`), PikoCI exposes:

| Variable               | Description                                    |
|------------------------|------------------------------------------------|
| `$BUILD_NUMBER`        | Sequential build number for the current job    |
| `$BUILD_JOB_NAME`      | Name of the job                                |
| `$BUILD_PIPELINE_NAME` | Name of the pipeline                           |
| `$WORKDIR`             | Temporary working directory for the job        |
| `$param_<name>`        | Per-job parameter overrides                    |

## Lifecycle

1. All `service` steps are collected from the job's plan
2. Services are started in order (each `start` command runs sequentially)
3. Ready checks run concurrently for all services that define one
4. If all services are ready, `get`, `task`, and `put` steps execute normally
5. After all steps complete (or if any step fails), `stop` runs for every started service

Services appear in the build as steps with type "service" and names like "postgres:start", "postgres:ready", "postgres:stop".

## Orphan prevention

If a worker crashes or restarts mid-job, the `stop` block never runs. Docker containers keep running. The next job run tries to start on the same port and fails because the port is already taken.

**The solution: stable container names + pre-start cleanup.**

Use a container name derived from `$BUILD_PIPELINE_NAME` and `$BUILD_JOB_NAME` instead of `$BUILD_NUMBER`. These are stable across runs. Always run cleanup at the top of the `start` block before starting the new container:

```bash
NAME="pikoci-${BUILD_PIPELINE_NAME}-${BUILD_JOB_NAME}-postgres"
docker rm -f $NAME 2>/dev/null || true   # kill orphan if exists
docker run -d --name $NAME ...           # start fresh
```

The `|| true` on the cleanup command means it never causes the start block to fail — only actual start failures will fail the job. The `stop` block should use the same pattern:

```bash
NAME="pikoci-${BUILD_PIPELINE_NAME}-${BUILD_JOB_NAME}-postgres"
docker rm -f $NAME 2>/dev/null || true
```

**Trade-off:** With stable names, only one instance of the service can run per pipeline/job combination at a time. If two builds of the same job run in parallel, the second one will kill the first's container. For most integration test use cases this is acceptable. If you need parallel isolation, append `$BUILD_NUMBER` to the name and accept the orphan risk.

## Examples

### Local process

Start PostgreSQL directly on the worker. No containers needed:

```hcl
service_type "postgres" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "initdb -D $WORKDIR/pgdata && pg_ctl -D $WORKDIR/pgdata -l $WORKDIR/pg.log start"]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", "pg_isready"]
    interval = "1s"
    timeout  = "30s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "pg_ctl -D $WORKDIR/pgdata stop"]
  }
}
```

### Docker container

Start a PostgreSQL container with orphan prevention:

```hcl
service_type "postgres" {
  params = ["version"]

  start "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-${BUILD_PIPELINE_NAME}-${BUILD_JOB_NAME}-pg"
      docker rm -f $NAME 2>/dev/null || true
      docker run -d --name $NAME -p 5432:5432 \
        -e POSTGRES_PASSWORD=test \
        postgres:$param_version
    EOT
    ]
  }

  ready_check "exec" {
    path     = "/bin/sh"
    args     = ["-ec", <<-EOT
      NAME="pikoci-${BUILD_PIPELINE_NAME}-${BUILD_JOB_NAME}-pg"
      docker exec $NAME pg_isready
    EOT
    ]
    interval = "2s"
    timeout  = "30s"
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", <<-EOT
      NAME="pikoci-${BUILD_PIPELINE_NAME}-${BUILD_JOB_NAME}-pg"
      docker rm -f $NAME 2>/dev/null || true
    EOT
    ]
  }
}
```

### Redis

A simple Redis instance with no ready check (starts fast enough):

```hcl
service_type "redis" {
  start "exec" {
    path = "/bin/sh"
    args = ["-ec", "redis-server --daemonize yes --dir $WORKDIR"]
  }

  stop "exec" {
    path = "/bin/sh"
    args = ["-ec", "redis-cli shutdown"]
  }
}
```
