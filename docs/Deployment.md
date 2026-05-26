# Deployment

This guide covers deploying PikoCI on a single server. This setup uses in-memory pubsub and an embedded worker, which is suitable for demos and small teams.

For production scaling with multiple workers or high availability, use an external queue backend and run workers separately. See [Queue Backends](Queue.md) and [Running Workers Separately](Workers.md).

## Quick start

Download or build the binary and run it:

```bash
pikoci server \
  --jwt-secret your-secret \
  --db-system sqlite \
  --db-name pikoci.db \
  --run-worker
```

PikoCI is now running on port 8080. That's it for a minimal deploy.

## Production single-server setup

This setup runs PikoCI as a bare binary managed by systemd, with supporting services (Caddy, Prometheus, Grafana, Node Exporter) in Docker Compose. This avoids Docker-in-Docker while keeping infrastructure containerized.

All config files are in the [`deploy/`](https://github.com/PikoCI/pikoci/tree/master/deploy) directory.

### 1. Install PikoCI

Copy the binary to the server:

```bash
# Build from source
GOOS=linux GOARCH=amd64 go build -o pikoci .
scp pikoci root@your-server:/usr/local/bin/pikoci

# Or download a release
curl -L https://github.com/PikoCI/pikoci/releases/latest/download/linux-amd64 -o /usr/local/bin/pikoci
chmod +x /usr/local/bin/pikoci
```

Alternatively, use the Docker image:

```bash
docker pull ghcr.io/pikoci/pikoci:latest
docker run -p 8080:8080 ghcr.io/pikoci/pikoci:latest server --db-system mem --pubsub-system mem --jwt-secret my-secret --run-worker
```

The image is based on Alpine and includes git, jq, curl, openssl, and docker-cli. See the [Dockerfile](https://github.com/PikoCI/pikoci/blob/master/Dockerfile) for details.

### 2. Install prerequisites

The server needs `git`, `jq`, and `curl` for the built-in resource types (e.g. the `git` resource type uses `jq` and `curl` to query GitHub/GitLab APIs). Docker is required for the `docker` runner and for supporting services.

```bash
apt-get install -y git jq curl
```

The deploy script (`deploy/deploy.sh`) installs these automatically if missing.

### 3. Create the system user and directories

```bash
useradd --system --no-create-home pikoci
mkdir -p /var/lib/pikoci /etc/pikoci
chown pikoci:pikoci /var/lib/pikoci
```

### 4. Configure PikoCI

Copy the example env file and fill in your values:

```bash
cp deploy/pikoci.env.example deploy/pikoci.env
```

Edit `deploy/pikoci.env` with your secrets:

```bash
JWT_SECRET=your-secure-random-secret
DB_SYSTEM=sqlite
DB_NAME=/var/lib/pikoci/pikoci.db
PUBSUB_SYSTEM=mem
RUN_WORKER=true
GF_SECURITY_ADMIN_PASSWORD=your-grafana-password
```

Override the default admin password:

```bash
pikoci user-password -u admin -p your-password
# Add the output to deploy/pikoci.env as:
# USERS=admin:$2a$10$...
```

`deploy/pikoci.env` is gitignored — only the `.example` file is tracked.

See [Server Configuration](Server.md) for all available options.

### 5. Deploy

The deploy script copies everything to the server — binary, configs, and env files:

```bash
./deploy/deploy.sh root@your-server
```

Or deploy manually:

```bash
# Install systemd unit
cp deploy/pikoci.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now pikoci
```

Verify it's running:

```bash
systemctl status pikoci
curl http://localhost:8080/metrics
```

### 6. Supporting services

The deploy script also syncs Docker Compose configs and env files. To start them manually:

```bash
cd /opt/pikoci
docker compose up -d
```

This starts:

- **Caddy** — reverse proxy with automatic HTTPS (ports 80/443)
- **Prometheus** — scrapes PikoCI `/metrics` and Node Exporter
- **Grafana** — dashboards (accessible at `grafana.pikoci.com`)
- **Node Exporter** — host-level metrics

### 7. DNS

Point your domain's A record to the server IP:

- `pikoci.com` (or your domain) → server IP
- `grafana.pikoci.com` → server IP

Caddy handles TLS certificates automatically via Let's Encrypt.

## Secrets management

- Keep your secrets in `deploy/pikoci.env` locally (gitignored). The deploy script copies it to the server with correct permissions (`chmod 600`)
- The `pikoci.env.example` file is a template — never commit actual secrets
- Generate JWT secrets with: `openssl rand -hex 32`
- Generate password hashes with: `pikoci user-password -u <user> -p <password>`
- For standalone workers, generate a worker token with `pikoci worker-token --jwt-secret <secret>` instead of distributing the raw JWT secret. See [Running Workers Separately](Workers.md)

## Monitoring

PikoCI exposes a `/metrics` endpoint in Prometheus format with:

- Go runtime metrics (goroutines, memory, GC)
- HTTP request counts by status code and method
- HTTP request duration histograms

The included `prometheus.yml` scrapes both PikoCI and Node Exporter. Add Prometheus as a data source in Grafana to build dashboards.

## Deploy script

The `deploy/deploy.sh` script automates the deploy process:

```bash
# Download latest release from GitHub and deploy
./deploy/deploy.sh root@your-server

# Or build from source instead
./deploy/deploy.sh --build root@your-server
```

It downloads the binary (or builds it with `--build`), copies it and all configs (including the env file) to the server, restarts the systemd service, and runs `docker compose up -d`.

## Self-deploy pipeline

PikoCI can deploy itself using graceful shutdown. The included `deploy/pipeline.hcl` has a `deploy` job that:

1. Waits for the `build-latest` Docker image job to pass
2. Builds a new binary from master
3. Copies it to `/usr/local/bin/pikoci`
4. Sends `SIGQUIT` to the running process

`SIGQUIT` triggers a graceful shutdown: PikoCI finishes the deploy job itself, then exits cleanly. Because the systemd unit uses `Restart=always`, systemd automatically restarts PikoCI with the new binary.

```hcl
job "deploy" {
  get "git" "pikoci_master" {
    trigger = true
    passed  = ["build-latest"]
  }
  task "deploy" {
    run "exec" {
      path = "/bin/sh"
      args = [
        "-ec",
        <<-EOT
        cd ${var.git_name}
        GOOS=linux GOARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') go build -o /tmp/pikoci-new .
        sudo cp /tmp/pikoci-new /usr/local/bin/pikoci
        sudo kill -QUIT $(pidof pikoci)
        EOT
      ]
    }
  }
}
```

See [Server Configuration — Signal handling](Server.md#signal-handling) for details on `SIGQUIT` vs `SIGTERM` behavior.
