# Portability and Bundling

PikoCI is designed to be fully portable. A single binary, a pipeline config, and optionally a SQLite database file are all you need.

## Minimal deployment

```bash
# Just these files:
pikoci              # the binary
pipeline.hcl        # your pipeline config
```

Run everything in memory:

```bash
./pikoci server \
  --jwt-secret my-secret \
  --db-system mem \
  --pubsub-system mem \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl
```

## Bundle with SQLite

For persistent state, add a SQLite database:

```bash
# These files:
pikoci              # the binary
pipeline.hcl        # your pipeline config
pikoci.db           # SQLite database (created on first run)
```

```bash
./pikoci server \
  --jwt-secret my-secret \
  --db-system sqlite \
  --db-name pikoci.db \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl
```

Copy the entire directory to another machine and it runs identically.

## Bundle with variables

```bash
pikoci
pipeline.hcl
vars.json           # variable values
pikoci.db
```

```bash
./pikoci server \
  --jwt-secret my-secret \
  --db-system sqlite \
  --db-name pikoci.db \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl \
  --vars vars.json
```

## Cross-platform builds

Build for any supported platform:

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o pikoci .

# macOS
GOOS=darwin GOARCH=amd64 go build -o pikoci .

# ARM (Raspberry Pi, etc.)
GOOS=linux GOARCH=arm64 go build -o pikoci .
```

## Docker (optional)

While PikoCI doesn't require Docker, you can containerize it:

```dockerfile
FROM scratch
COPY pikoci /pikoci
COPY pipeline.hcl /pipeline.hcl
ENTRYPOINT ["/pikoci", "server", "--jwt-secret", "my-secret", "--pipeline-name", "my-pipeline", "--pipeline-config", "/pipeline.hcl"]
```

## Key point

PikoCI has no external service dependencies. No Docker Compose, no Kubernetes, no setup scripts. Download the binary, write your pipeline, and run it.
