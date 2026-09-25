#!/usr/bin/env sh
# container_e2e.sh drives the built image over real HTTP.
#
# It starts the OpenAI-compatible stub (scripts/stub_llm.py) as a container on a
# private Docker network shared with the harness container, builds the harness
# image, runs it, then exercises the async API end to end: submit a triage job,
# poll to completion, and assert the classification. This is the scheduled
# "does the shipped artifact work" check; it is deliberately not in required CI
# (it needs Docker and is slower).
#
# The stub runs in a container rather than on the host because a host firewall
# (UFW/nftables) commonly drops container-to-host traffic, which would make the
# harness unable to reach a host-bound stub.
#
# Env:
#   IMAGE        image tag to build (default j-harness:e2e)
#   PORT         host port to publish the harness on (default 18080)
#   STUB_IMAGE   image used to run the stub (default python:3.12-alpine)
#   NET          Docker network name (default j-harness-e2e-net)

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE=${IMAGE:-j-harness:e2e}
PORT=${PORT:-18080}
STUB_IMAGE=${STUB_IMAGE:-python:3.12-alpine}
NET=${NET:-j-harness-e2e-net}
HARNESS_NAME=jh-e2e-harness
STUB_NAME=jh-e2e-stub

cleanup() {
  docker rm -f "$HARNESS_NAME" "$STUB_NAME" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup
docker network create "$NET" >/dev/null

echo "==> building image $IMAGE"
docker build --build-arg VERSION=e2e -t "$IMAGE" "$ROOT"

echo "==> starting stub llm container on network $NET"
docker run -d --name "$STUB_NAME" --network "$NET" \
  -v "$ROOT/scripts/stub_llm.py:/stub_llm.py:ro" \
  -e STUB_HOST=0.0.0.0 -e STUB_PORT=11500 \
  "$STUB_IMAGE" python3 /stub_llm.py >/dev/null

echo "==> running harness container on :$PORT"
docker run -d --name "$HARNESS_NAME" --network "$NET" -p "127.0.0.1:$PORT:8080" \
  -e HARNESS_ADDR=0.0.0.0:8080 \
  -e OPENAI_BASE_URL="http://$STUB_NAME:11500/v1" \
  -e OPENAI_API_KEY=stub \
  "$IMAGE" >/dev/null

echo "==> waiting for healthz"
i=0
while [ "$i" -lt 50 ]; do
  if curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then break; fi
  i=$((i + 1))
  sleep 0.2
done
curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null || {
  echo "FAIL: container did not become healthy" >&2
  docker logs "$HARNESS_NAME" >&2 || true
  exit 1
}

echo "==> submitting triage job"
SID=$(curl -sf "http://127.0.0.1:$PORT/v1/agents/triage/execute" \
  -H 'Content-Type: application/json' \
  -d '{"input_data":"I was charged twice on my invoice and need a refund."}' \
  | sed -n 's/.*"session_id":"\([^"]*\)".*/\1/p')
[ -n "$SID" ] || { echo "FAIL: no session_id" >&2; exit 1; }
echo "session: $SID"

echo "==> polling session"
i=0
STATUS=""
BODY=""
while [ "$i" -lt 100 ]; do
  BODY=$(curl -sf "http://127.0.0.1:$PORT/v1/sessions/$SID" || true)
  STATUS=$(printf '%s' "$BODY" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
  case "$STATUS" in
    COMPLETED | FAILED) break ;;
  esac
  i=$((i + 1))
  sleep 0.2
done

echo "session body: $BODY"
[ "$STATUS" = "COMPLETED" ] || {
  echo "FAIL: status=$STATUS" >&2
  docker logs "$HARNESS_NAME" >&2 || true
  exit 1
}
printf '%s' "$BODY" | tr -d ' ' | grep -q '"category\\":\\"billing\\"' ||
  printf '%s' "$BODY" | tr -d ' ' | grep -q '"category":"billing"' ||
  { echo "FAIL: expected billing classification in $BODY" >&2; exit 1; }

echo "==> checking step results endpoint"
curl -sf "http://127.0.0.1:$PORT/v1/sessions/$SID/steps" | grep -q '"step_id":"triage"' ||
  { echo "FAIL: step results missing triage" >&2; exit 1; }

echo "container e2e passed"
