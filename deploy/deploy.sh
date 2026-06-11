#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# deploy.sh — Deploy PikoCI to a remote server
# =============================================================================
#
# This script handles the full deployment of PikoCI and its supporting services
# (Caddy, Prometheus, Grafana, Node Exporter) to a remote Linux server via SSH.
#
# It is idempotent: safe to run repeatedly. Each run stops the running service,
# replaces the binary and configs, and restarts everything.
#
# PREREQUISITES:
#   - SSH access to the target server (configure in ~/.ssh/config)
#   - deploy/pikoci.env must exist (copy from deploy/pikoci.env.example)
#   - Go toolchain (only if using --build)
#
# USAGE:
#   ./deploy.sh [--build] <ssh-host>
#
# OPTIONS:
#   --build    Build the binary from source instead of downloading the latest
#              GitHub release. Requires Go installed locally.
#
# EXAMPLES:
#   ./deploy.sh pikoci                 # Download latest release, deploy
#   ./deploy.sh --build pikoci         # Build from source, deploy
#   ./deploy.sh root@94.130.151.123    # Deploy using IP directly
#
# WHAT IT DOES (in order):
#   1. Validates that deploy/pikoci.env exists
#   2. Detects the server's CPU architecture (amd64 or arm64)
#   3. Obtains the PikoCI binary (download release or build from source)
#   4. Stops the running PikoCI and worker systemd services (if any)
#   5. Copies the binary to /usr/local/bin/pikoci on the server
#   6. Creates server directories, system user, and sets ownership:
#      - /opt/pikoci      — Docker Compose configs and supporting services
#      - /etc/pikoci       — PikoCI environment/config (restricted permissions)
#      - /var/lib/pikoci   — PikoCI data directory (owned by pikoci user)
#   7. Copies config files to the server:
#      - pikoci.service, pikoci-worker@.service → /etc/systemd/system/
#      - docker-compose.yml, Caddyfile, prometheus.yml → /opt/pikoci/
#   8. Copies the env file to two locations:
#      - /etc/pikoci/pikoci.env  — read by systemd (chmod 600)
#      - /opt/pikoci/pikoci.env  — read by Docker Compose containers (chmod 600)
#      - /opt/pikoci/.env        — filtered subset (PIKOCI_DOMAIN, GF_*) for
#                                  Docker Compose variable interpolation in
#                                  docker-compose.yml (avoids bcrypt $ warnings)
#   9. Installs Docker on the server if not already present
#  10. Runs docker compose up -d for supporting services
#  11. Reloads systemd and restarts PikoCI server + worker services
#
# SERVER LAYOUT:
#   /usr/local/bin/pikoci              — PikoCI binary
#   /etc/systemd/system/pikoci.service — systemd unit (server)
#   /etc/systemd/system/pikoci-worker@.service — systemd template (workers)
#   /etc/pikoci/pikoci.env             — env vars for PikoCI (secrets)
#   /var/lib/pikoci/                   — PikoCI data (SQLite DB, XDG data)
#   /opt/pikoci/                       — Docker Compose stack
#   /opt/pikoci/docker-compose.yml     — Caddy, Prometheus, Grafana, Node Exporter
#   /opt/pikoci/Caddyfile              — reverse proxy config
#   /opt/pikoci/prometheus.yml         — Prometheus scrape targets
#   /opt/pikoci/pikoci.env             — env vars for Docker Compose containers
#   /opt/pikoci/.env                   — filtered env vars for Compose interpolation
#
# =============================================================================

BUILD=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --build)
            BUILD=true
            shift
            ;;
        -*)
            echo "Unknown option: $1"
            exit 1
            ;;
        *)
            SSH_HOST="$1"
            shift
            ;;
    esac
done

if [ -z "${SSH_HOST:-}" ]; then
    echo "Usage: $0 [--build] <ssh-host>"
    echo "Example: $0 root@pikoci.com"
    exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOY_DIR="$REPO_ROOT/deploy"

# --- Validate local prerequisites ---

if [ ! -f "$DEPLOY_DIR/pikoci.env" ]; then
    echo "Error: deploy/pikoci.env not found."
    echo "Create it from the template: cp deploy/pikoci.env.example deploy/pikoci.env"
    exit 1
fi

# --- Detect server architecture ---
# PikoCI is a Go binary; we need to match the target CPU.

SERVER_ARCH=$(ssh "$SSH_HOST" 'uname -m')
case "$SERVER_ARCH" in
    x86_64)  GOARCH=amd64 ;;
    aarch64) GOARCH=arm64 ;;
    *)
        echo "Error: unsupported server architecture: $SERVER_ARCH"
        exit 1
        ;;
esac
BINARY="$REPO_ROOT/builds/pikoci-linux-$GOARCH"

# --- Obtain binary ---

if [ "$BUILD" = true ]; then
    echo "==> Building PikoCI binary (linux/$GOARCH)..."
    cd "$REPO_ROOT"
    GOOS=linux GOARCH="$GOARCH" go build -o "$BINARY" .
else
    echo "==> Downloading latest PikoCI release (linux/$GOARCH)..."
    mkdir -p "$REPO_ROOT/builds"
    curl -fSL "https://github.com/pikoci/pikoci/releases/latest/download/linux-$GOARCH" -o "$BINARY"
    chmod +x "$BINARY"
fi

# --- Stop running service ---
# Must stop before copying to avoid "text file busy" errors.

echo "==> Stopping PikoCI services..."
ssh "$SSH_HOST" 'systemctl stop pikoci-worker@worker-1 pikoci-worker@worker-2 2>/dev/null || true'
ssh "$SSH_HOST" 'systemctl stop pikoci 2>/dev/null || true'

# --- Copy binary ---

echo "==> Copying binary to $SSH_HOST..."
scp "$BINARY" "$SSH_HOST":/usr/local/bin/pikoci
ssh "$SSH_HOST" 'chown pikoci:pikoci /usr/local/bin/pikoci'

# --- Sync deploy configs ---
# Creates directories and the pikoci system user on first run.

echo "==> Syncing deploy configs..."
ssh "$SSH_HOST" 'mkdir -p /opt/pikoci /etc/pikoci /var/lib/pikoci /var/www/pikoci.com /var/www/docs.pikoci.com && id -u pikoci &>/dev/null || useradd --system --no-create-home pikoci && chown pikoci:pikoci /var/lib/pikoci /var/www/pikoci.com /var/www/docs.pikoci.com && usermod -aG docker pikoci 2>/dev/null || true'
scp "$DEPLOY_DIR/pikoci.service" "$SSH_HOST":/etc/systemd/system/pikoci.service
scp "$DEPLOY_DIR/pikoci-worker@.service" "$SSH_HOST":/etc/systemd/system/pikoci-worker@.service
scp "$DEPLOY_DIR/docker-compose.yml" "$SSH_HOST":/opt/pikoci/docker-compose.yml
scp "$DEPLOY_DIR/Caddyfile" "$SSH_HOST":/opt/pikoci/Caddyfile
scp "$DEPLOY_DIR/prometheus.yml" "$SSH_HOST":/opt/pikoci/prometheus.yml
ssh "$SSH_HOST" 'mkdir -p /opt/pikoci/grafana/provisioning/datasources /opt/pikoci/grafana/provisioning/dashboards /opt/pikoci/grafana/dashboards'
scp "$DEPLOY_DIR/grafana/provisioning/datasources/prometheus.yml" "$SSH_HOST":/opt/pikoci/grafana/provisioning/datasources/prometheus.yml
scp "$DEPLOY_DIR/grafana/provisioning/dashboards/default.yml" "$SSH_HOST":/opt/pikoci/grafana/provisioning/dashboards/default.yml
scp "$DEPLOY_DIR/grafana/dashboards/pikoci.json" "$SSH_HOST":/opt/pikoci/grafana/dashboards/pikoci.json

# --- Sync env file ---
# The full env file goes to two places:
#   /etc/pikoci/pikoci.env  — systemd reads this via EnvironmentFile=
#   /opt/pikoci/pikoci.env  — Docker Compose containers read this via env_file:
#
# A filtered .env is created for Docker Compose YAML interpolation (${VAR}).
# Only PIKOCI_DOMAIN, PIKOCI_GRAFANA_DOMAIN, and GF_* vars are included to
# avoid warnings from bcrypt $ characters in USERS values.

echo "==> Syncing env file..."
scp "$DEPLOY_DIR/pikoci.env" "$SSH_HOST":/etc/pikoci/pikoci.env
ssh "$SSH_HOST" 'chown pikoci:pikoci /etc/pikoci/pikoci.env && chmod 600 /etc/pikoci/pikoci.env'
scp "$DEPLOY_DIR/pikoci.env" "$SSH_HOST":/opt/pikoci/pikoci.env
ssh "$SSH_HOST" 'chmod 600 /opt/pikoci/pikoci.env'
ssh "$SSH_HOST" 'rm -f /opt/pikoci/.env && grep -E "^(PIKOCI_DOMAIN|PIKOCI_GRAFANA_DOMAIN|PIKOCI_WWW_DOMAIN|PIKOCI_DOCS_DOMAIN|GF_)" /opt/pikoci/pikoci.env > /opt/pikoci/.env'

# Copy GitHub App PEM key if present
if [ -f "$DEPLOY_DIR/pikoci_github_app.pem" ]; then
    echo "==> Syncing GitHub App PEM key..."
    scp "$DEPLOY_DIR/pikoci_github_app.pem" "$SSH_HOST":/etc/pikoci/pikoci_github_app.pem
    ssh "$SSH_HOST" 'chown pikoci:pikoci /etc/pikoci/pikoci_github_app.pem && chmod 600 /etc/pikoci/pikoci_github_app.pem'
fi

# --- Build and deploy docs ---

echo "==> Building docs..."
cd "$REPO_ROOT"
python3 -m venv "$REPO_ROOT/.venv" 2>/dev/null || true
"$REPO_ROOT/.venv/bin/pip" install --quiet mkdocs-material
"$REPO_ROOT/.venv/bin/mkdocs" build --clean
echo "==> Uploading docs to $SSH_HOST..."
ssh "$SSH_HOST" 'mkdir -p /var/www/docs.pikoci.com'
rsync -a --delete "$REPO_ROOT/site/" "$SSH_HOST":/var/www/docs.pikoci.com/
ssh "$SSH_HOST" 'chown -R pikoci:pikoci /var/www/docs.pikoci.com /var/www/pikoci.com 2>/dev/null || true'

# --- Install required packages ---

if ! ssh "$SSH_HOST" 'command -v jq &>/dev/null'; then
    echo "==> Installing jq on $SSH_HOST..."
    ssh "$SSH_HOST" 'apt-get install -y jq || apk add jq || dnf install -y jq'
fi

if ! ssh "$SSH_HOST" 'command -v dot &>/dev/null'; then
    echo "==> Installing graphviz on $SSH_HOST..."
    ssh "$SSH_HOST" 'apt-get install -y graphviz || apk add graphviz || dnf install -y graphviz'
fi

if ! ssh "$SSH_HOST" 'command -v git &>/dev/null'; then
    echo "==> Installing git on $SSH_HOST..."
    ssh "$SSH_HOST" 'apt-get install -y git || apk add git || dnf install -y git'
fi

if ! ssh "$SSH_HOST" 'python3 -c "import venv" 2>/dev/null'; then
    echo "==> Installing python3-venv on $SSH_HOST..."
    ssh "$SSH_HOST" 'apt-get install -y python3-venv || apk add python3 || dnf install -y python3'
fi

if ! ssh "$SSH_HOST" 'command -v docker &>/dev/null'; then
    echo "==> Installing Docker on $SSH_HOST..."
    ssh "$SSH_HOST" 'curl -fsSL https://get.docker.com | sh'
fi

# --- Restart services ---

echo "==> Updating Docker Compose services..."
ssh "$SSH_HOST" 'cd /opt/pikoci && docker compose up -d'

echo "==> Restarting PikoCI services..."
ssh "$SSH_HOST" 'systemctl daemon-reload && systemctl restart pikoci && systemctl enable --now pikoci-worker@worker-1 pikoci-worker@worker-2'

echo "==> Deploy complete."
