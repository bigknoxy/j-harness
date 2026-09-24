#!/usr/bin/env sh
# Smoke test: build, boot, hit /healthz, shut down.
set -eu

ADDR="${HARNESS_ADDR:-127.0.0.1:8081}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DB="$(mktemp -d)/harness.db"

cd "$ROOT"
make build

HARNESS_DB="$DB" ./bin/harness --addr "$ADDR" &
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
echo "/readyz  -> $(curl -s "http://$ADDR/readyz")"

# /metrics should render Prometheus text.
curl -sf "http://$ADDR/metrics" | grep -q "harness_jobs_submitted_total" \
  || { echo "expected /metrics to expose harness_jobs_submitted_total"; exit 1; }

# Unknown session should be a clean 404 (exercises the store path).
code=$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR/v1/sessions/does-not-exist")
[ "$code" = "404" ] || { echo "expected 404 for unknown session, got $code"; exit 1; }

echo "smoke test passed"
