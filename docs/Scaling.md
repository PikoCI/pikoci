# Scaling PikoCI

PikoCI is designed to grow with your needs. You can start with a single binary
and zero external dependencies, then scale to distributed workers and
production-grade databases without changing your pipeline config.

---

## Phase 1 — In-memory, single machine

The fastest way to get started. Everything runs in a single process.
No files, no external services, no configuration.

```bash
pikoci server \
  --db-system mem \
  --pubsub-system mem \
  --run-worker \
  --jwt-secret my-secret \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl
```

**What this gives you:**
- Server + worker in one process
- Pipeline loaded at startup, ready immediately
- Zero external dependencies

**The trade-off:**
- All data is lost on restart
- Only one worker

Good for: local development, trying things out, CI for small personal projects.

---

## Phase 2 — SQLite, single machine

Add persistence without any new infrastructure. One flag change.

```bash
pikoci server \
  --db-system sqlite \
  --db-name pikoci.db \
  --pubsub-system mem \
  --run-worker \
  --jwt-secret my-secret \
  --pipeline-name my-pipeline \
  --pipeline-config pipeline.hcl
```

**What this gives you:**
- Build history survives restarts
- Resource versions and cursors persist
- Still zero external dependencies

**Note:** From Phase 2 onward, pipelines persist in the database. You can
manage them via the web UI or CLI (`pikoci client pipelines create ...`)
instead of passing `--pipeline-name` / `--pipeline-config` on every start.
The flags still work — they create or update the pipeline at startup.

**Migrating from in-memory:**

If you started with in-memory and want to keep your data, export it first:

```bash
# while the server is still running
pikoci client export --url http://localhost:8080 --output pikoci.db

# stop the server, restart with SQLite pointing at the exported file
pikoci server --db-system sqlite --db-name pikoci.db ...
```

Good for: teams that want history, projects that need to survive restarts.

---

## Phase 3 — Distributed workers with NATS

Add more workers without changing the server. Workers can run on any machine
that has network access to the server and to NATS.

**Why NATS:**
NATS is the simplest external queue to set up — a single binary, no
configuration needed for basic use. If you don't have a queue already,
start with NATS.

**Step 1 — Start NATS:**

```bash
# via Docker
docker run -d -p 4222:4222 nats

# or download the binary
nats-server
```

**Step 2 — Restart the server without a built-in worker:**

```bash
export NATS_SERVER_URL="nats://localhost:4222"

pikoci server \
  --db-system sqlite \
  --db-name pikoci.db \
  --pubsub-system nats \
  --run-worker=false \
  --jwt-secret my-secret
```

**Step 3 — Start workers on any machine:**

```bash
# generate a worker token (or copy it from the server startup logs)
pikoci worker-token --jwt-secret my-secret

# start worker 1 (same machine or different)
export NATS_SERVER_URL="nats://your-nats-host:4222"

pikoci worker \
  --pikoci-url http://your-server:8080 \
  --pubsub-system nats \
  --worker-token <token>

# start worker 2
pikoci worker \
  --pikoci-url http://your-server:8080 \
  --pubsub-system nats \
  --worker-token <token>
```

Add as many workers as you need. NATS distributes jobs between them
automatically. Workers can be on different machines, in different networks,
or behind NAT — they only need outbound access to NATS and the server API.

**Dedicated workers with `--queues`:**

PikoCI uses two separate queues: one for jobs and one for resource checks.
You can run specialized workers to prevent long-running jobs from blocking
resource version detection:

```bash
# worker that only runs jobs (e.g. on a beefy machine)
pikoci worker --queues jobs --pubsub-system nats --worker-token <token> ...

# worker that only checks resources (lightweight, never blocked by jobs)
pikoci worker --queues checks --pubsub-system nats --worker-token <token> ...

# worker that handles both (default)
pikoci worker --pubsub-system nats --worker-token <token> ...
```

**What this gives you:**
- Multiple workers running jobs in parallel
- Workers can be on any machine
- Workers can come and go without affecting the server
- Jobs survive worker crashes — NATS requeues unacknowledged messages

**Note:** Switching from `--pubsub-system mem` to `nats` means the queue
changes. Any builds that were pending in the in-memory queue will not carry
over — they'll need to be retriggered.

Good for: teams with multiple projects, jobs that need specific hardware,
isolating workloads.

---

## Phase 4 — Production with PostgreSQL

Replace SQLite with PostgreSQL for better performance, concurrent access,
and the ability to run multiple server instances.

```bash
export NATS_SERVER_URL="nats://nats-host:4222"

pikoci server \
  --db-system postgresql \
  --db-host db.example.com \
  --db-port 5432 \
  --db-user pikoci \
  --db-password secret \
  --db-name pikoci \
  --pubsub-system nats \
  --run-worker=false \
  --jwt-secret my-secret
```

**Migrating from SQLite to PostgreSQL:**

[pgloader](https://pgloader.io/) handles SQLite to PostgreSQL migration in
one command. Install it first (`apt install pgloader`, `brew install pgloader`,
or use the Docker image), then:

```bash
pgloader pikoci.db postgresql://user:pass@host/pikoci
```

PikoCI runs database migrations automatically on startup, so after pgloader
copies the data, just restart the server with `--db-system postgresql` and
migrations will bring the schema up to date if needed.

**Multiple server instances:**

With PostgreSQL you can run multiple server instances behind a load balancer.
The DB-backed scheduler uses `SELECT ... FOR UPDATE SKIP LOCKED` to ensure
each resource check is processed by only one instance.

```bash
# instance 1
pikoci server --db-system postgresql --db-host db.example.com --db-name pikoci \
  --pubsub-system nats --run-worker=false --jwt-secret my-secret ...

# instance 2 (same config, different machine)
pikoci server --db-system postgresql --db-host db.example.com --db-name pikoci \
  --pubsub-system nats --run-worker=false --jwt-secret my-secret ...
```

**What this gives you:**
- High availability — server instances can restart without downtime
- Better performance for large builds and many pipelines
- Multiple server instances behind a load balancer

Good for: production deployments, teams that need HA, large-scale CI.

---

## Summary

| Phase | Database | Queue | Workers | Use case |
|-------|----------|-------|---------|----------|
| 1 | Memory | Memory | Built-in | Development, trying things out |
| 2 | SQLite | Memory | Built-in | Small teams, persistence needed |
| 3 | SQLite | NATS | Distributed | Multiple workers, any machine |
| 4 | PostgreSQL | NATS | Distributed | Production, high availability |

The pipeline config never changes between phases.
Add infrastructure when you need it — not before.

---

## Other supported backends

**Databases:** SQLite, MySQL, MariaDB, PostgreSQL

**Queues:** in-memory, NATS, RabbitMQ, Kafka

See [Database Backends](Database.md) and [Queue Backends](Queue.md) for full configuration reference.
