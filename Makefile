BINARY := harness
PKG := ./...
GOFLAGS := -trimpath

.PHONY: all build run test vet fmt fmt-check lint tidy clean docker e2e eval docs coverage

all: fmt-check vet test build

build:
	@mkdir -p bin
	go build $(GOFLAGS) -o bin/$(BINARY) ./cmd/harness

run:
	go run ./cmd/harness

test:
	go test $(GOFLAGS) -race ./...

# e2e drives the full HTTP stack against an in-process OpenAI-compatible stub.
# It is also part of `test`; this target runs it alone, offline and fast.
e2e:
	go test $(GOFLAGS) -race ./internal/e2e/...

# eval runs the checked-in deterministic eval suite (no network, no model).
eval:
	go test $(GOFLAGS) -race ./internal/eval/...

# docs is the pure-Go drift gate: routes/env vars/metrics in code vs docs,
# relative markdown links, version pin, and the ASCII-only rule. Offline.
docs:
	go test $(GOFLAGS) ./internal/docscheck/...

# coverage runs the suite with a coverage profile and enforces the statement
# floor from scripts/coverage.sh (COVERAGE_THRESHOLD, default 70). Set
# HARNESS_REDIS_ADDR to a live server to include the Redis store tests.
coverage:
	sh scripts/coverage.sh

vet:
	go vet $(PKG)

fmt:
	gofmt -w .

fmt-check:
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then echo "unformatted files:"; echo "$$files"; exit 1; fi

lint: vet fmt-check

tidy:
	go mod tidy

clean:
	rm -rf bin

docker:
	docker build -t j-harness:local .
