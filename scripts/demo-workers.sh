#!/usr/bin/env bash
#
# Starts 1 PikoCI server (no embedded worker) + 2 standalone workers via NATS,
# then kills one worker after 20 seconds.
#
# Prerequisites:
#   - NATS running on localhost:4222 (run: make nats-up)
#   - Go toolchain available
#
# Usage:
#   ./scripts/demo-workers.sh

set -euo pipefail

JWT_SECRET="demo-secret-for-testing"
NATS_URL="nats://localhost:4222"
SERVER_PORT=8080
BIN="/tmp/pikoci-demo"
PIDS=()

cleanup() {
  echo ""
  echo "==> Cleaning up..."
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  wait 2>/dev/null || true
  echo "==> Done."
}
trap cleanup EXIT

export NATS_SERVER_URL="$NATS_URL"

echo "==> Building pikoci..."
go build -o "$BIN" .

# Generate worker token and admin user entry (username:hash)
WORKER_TOKEN=$("$BIN" worker-token --jwt-secret "$JWT_SECRET")
ADMIN_ENTRY=$("$BIN" user-password -u admin -p admin123)

echo "==> Starting server (port $SERVER_PORT, no embedded worker)..."
"$BIN" server \
  --jwt-secret "$JWT_SECRET" \
  --port "$SERVER_PORT" \
  --run-worker=false \
  --pubsub-system nats \
  --users "${ADMIN_ENTRY}" \
  > >(sed 's/^/[server]   /') 2>&1 &
SERVER_PID=$!
PIDS+=("$SERVER_PID")

echo "==> Waiting for server..."
for _ in $(seq 1 30); do
  if curl -sf "http://localhost:$SERVER_PORT/version" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
echo "==> Server ready."

echo "==> Starting worker-1..."
"$BIN" worker \
  --pikoci-url "http://localhost:$SERVER_PORT" \
  --worker-token "$WORKER_TOKEN" \
  --pubsub-system nats \
  --name worker-1 \
  --queues jobs,checks \
  > >(sed 's/^/[worker-1] /') 2>&1 &
WORKER1_PID=$!
PIDS+=("$WORKER1_PID")

echo "==> Starting worker-2..."
"$BIN" worker \
  --pikoci-url "http://localhost:$SERVER_PORT" \
  --worker-token "$WORKER_TOKEN" \
  --pubsub-system nats \
  --name worker-2 \
  --queues jobs,checks \
  > >(sed 's/^/[worker-2] /') 2>&1 &
WORKER2_PID=$!
PIDS+=("$WORKER2_PID")

echo ""
echo "  Server PID:   $SERVER_PID"
echo "  Worker-1 PID: $WORKER1_PID"
echo "  Worker-2 PID: $WORKER2_PID"
echo "  UI: http://localhost:$SERVER_PORT/#workers  (admin / admin123)"
echo ""
echo "==> Killing worker-2 in 20 seconds..."
sleep 20

echo ""
echo "==> Killing worker-2 (PID $WORKER2_PID)..."
kill "$WORKER2_PID" 2>/dev/null || true
wait "$WORKER2_PID" 2>/dev/null || true

echo "==> Worker-2 killed. worker-1 should show healthy, worker-2 will turn stale after ~90s."
echo "==> Press Ctrl+C to stop everything."

wait
