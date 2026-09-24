# --- build stage ---
FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/harness ./cmd/harness

# --- runtime stage ---
FROM alpine:3.20
RUN apk --no-cache add ca-certificates && adduser -D -u 10001 harness
WORKDIR /app
COPY --from=builder /out/harness /usr/local/bin/harness
COPY agent-registry ./agent-registry
COPY schemas ./schemas
RUN mkdir -p /app/data && chown -R harness:harness /app
USER harness
ENV HARNESS_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["harness"]
