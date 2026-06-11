# PikoCI Examples

Ready-to-run example pipelines for PikoCI.

## Quick Start

The fastest way to try PikoCI:

```bash
cd examples
docker compose up
```

Open [http://localhost:8080](http://localhost:8080) and log in with `admin` / `admin123`. The hello-world pipeline runs automatically every 10 seconds.

## Examples

### hello-world

Simplest possible pipeline. A cron resource triggers a job that runs `echo` every 10 seconds. Uses the exec runner — no Docker required inside the container.

```bash
# Run standalone (without Docker)
pikoci server \
  --db-system mem \
  --jwt-secret my-secret \
  --run-worker \
  --pipeline-name hello-world \
  --pipeline-config examples/hello-world/pipeline.hcl
```

### go-project

A more realistic pipeline that watches a public Go repository, runs lint and test in parallel using the Docker runner, then builds only after both pass (using `passed` constraints).

Requires Docker to be available (the Docker runner executes jobs inside containers).

```bash
pikoci server \
  --db-system mem \
  --jwt-secret my-secret \
  --run-worker \
  --pipeline-name go-project \
  --pipeline-config examples/go-project/pipeline.hcl
```
