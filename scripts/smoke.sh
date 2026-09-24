#!/usr/bin/env sh
# Smoke test: build, boot, hit /healthz, shut down.
set -eu

ADDR="${HARNESS_ADDR:-127.0.0.1:8081}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$ROOT"
make build

./bin/harness --addr "$ADDR" &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT

# wait for readiness
i=0
until curl -sf "http://$ADDR/healthz" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -gt 50 ] && { echo "harness did not become healthy"; exit 1; }
  sleep 0.1
done

echo "/healthz -> $(curl -s "http://$ADDR/healthz")"
echo "smoke test passed"
