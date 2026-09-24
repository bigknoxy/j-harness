BINARY := harness
PKG := ./...
GOFLAGS := -trimpath

.PHONY: all build run test vet fmt fmt-check lint tidy clean docker

all: fmt-check vet test build

build:
	@mkdir -p bin
	go build $(GOFLAGS) -o bin/$(BINARY) ./cmd/harness

run:
	go run ./cmd/harness

test:
	go test $(GOFLAGS) -race ./...

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
