#!/usr/bin/env sh
# eval_live.sh replays a few agent eval cases against a real OpenAI-compatible
# model. It is the nightly counterpart to the offline `internal/eval` suite:
# same intent (does the registry behave), real model, real HTTP.
#
# Env (required):
#   OPENAI_API_KEY   API key for the endpoint
#   OPENAI_BASE_URL  e.g. https://integrate.api.nvidia.com/v1
#   OPENAI_MODEL     model id
#
# Env (optional):
#   ADDR  harness listen address (default 127.0.0.1:18090)

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ADDR=${ADDR:-127.0.0.1:18090}
DB=$(mktemp -d)/live.db

if [ -z "${OPENAI_API_KEY:-}" ] || [ -z "${OPENAI_BASE_URL:-}" ] || [ -z "${OPENAI_MODEL:-}" ]; then
  echo "OPENAI_API_KEY, OPENAI_BASE_URL and OPENAI_MODEL are required" >&2
  exit 1
fi

cleanup() {
  [ -n "${PID:-}" ] && kill "$PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> building"
(cd "$ROOT" && make build >/dev/null)

echo "==> starting harness against $OPENAI_BASE_URL ($OPENAI_MODEL)"
HARNESS_DB=$DB ENABLE_TOOLS=true "$ROOT/bin/harness" --addr "$ADDR" --registry "$ROOT/agent-registry" &
PID=$!

i=0
while [ "$i" -lt 50 ]; do
  if curl -sf "http://$ADDR/healthz" >/dev/null 2>&1; then break; fi
  i=$((i + 1))
  sleep 0.2
done
curl -sf "http://$ADDR/healthz" >/dev/null || { echo "harness did not start" >&2; exit 1; }

# case: message -> expected triage category
check() {
  msg=$1
  want=$2
  sid=$(curl -sf "http://$ADDR/v1/agents/triage/execute" \
    -H 'Content-Type: application/json' \
    -d "{\"input_data\":$(printf '%s' "$msg" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')}" \
    | sed -n 's/.*"session_id":"\([^"]*\)".*/\1/p')
  [ -n "$sid" ] || { echo "FAIL: no session id for '$msg'" >&2; return 1; }

  j=0
  while [ "$j" -lt 150 ]; do
    body=$(curl -sf "http://$ADDR/v1/sessions/$sid" || true)
    status=$(printf '%s' "$body" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
    case "$status" in COMPLETED | FAILED) break ;; esac
    j=$((j + 1))
    sleep 0.5
  done

  if [ "$status" != "COMPLETED" ]; then
    echo "FAIL: '$msg' status=$status body=$body" >&2
    return 1
  fi
  printf '%s' "$body" | tr -d ' ' | grep -q "\"category\\\\\":\\\\\"$want\\\\\"" ||
    printf '%s' "$body" | tr -d ' ' | grep -q "\"category\":\"$want\"" ||
    { echo "FAIL: '$msg' expected category=$want, got $body" >&2; return 1; }
  echo "ok: $want <- $msg"
}

check "I was charged twice on my invoice and need a refund." billing
check "The app crashes with a stack trace on startup." technical

echo "live eval passed"
