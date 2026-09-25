#!/usr/bin/env sh
# coverage.sh runs the full test suite with coverage and fails if the total
# statement coverage drops below COVERAGE_THRESHOLD (default 70).
#
# It is intentionally pure Go: `go test -coverprofile` plus `go tool cover`.
# No coverage service or new dependency is involved. When HARNESS_REDIS_ADDR
# points at a live server the Redis store tests run and count toward coverage.
set -eu

THRESHOLD="${COVERAGE_THRESHOLD:-70}"
PROFILE="${COVERAGE_PROFILE:-coverage.out}"

# /tmp can be a slow filesystem on some hosts; prefer a tmpfs when present.
if [ -z "${TMPDIR:-}" ] && [ -d /dev/shm ]; then
  TMPDIR=/dev/shm
  export TMPDIR
fi

echo "running tests with coverage (threshold ${THRESHOLD}%)..."
go test -trimpath -covermode=atomic -coverprofile="${PROFILE}" ./...

TOTAL=$(go tool cover -func="${PROFILE}" | awk '/^total:/ {print $3}' | tr -d '%')
if [ -z "${TOTAL}" ]; then
  echo "could not read total coverage from ${PROFILE}" >&2
  exit 1
fi

echo "total coverage: ${TOTAL}%"

# Compare without bc/awk float tricks: strip the decimal, compare integers of
# percent x10 so 69.9 correctly fails a 70 threshold.
want=$(printf '%s' "${THRESHOLD}" | awk '{printf "%d", $1*10}')
got=$(printf '%s' "${TOTAL}" | awk '{printf "%d", $1*10}')

if [ "${got}" -lt "${want}" ]; then
  echo "FAIL: coverage ${TOTAL}% is below the ${THRESHOLD}% threshold" >&2
  exit 1
fi

echo "coverage gate passed"
