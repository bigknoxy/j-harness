# Deploying j-harness

The harness is a single static binary plus a file-based registry. Deployment is
therefore mostly about two things: shipping the binary (or image) and mounting a
registry directory that git owns.

## Build the image

```sh
docker build -t j-harness:local .
# or, to stamp a version into `harness --version`:
docker build --build-arg VERSION=0.1.0 -t j-harness:0.1.0 .
```

The image is multi-stage: a `golang:1.27-alpine` builder compiles with
`CGO_ENABLED=0` (so the pure-Go SQLite driver needs no libc), and the runtime is
`alpine:3.20` running as the non-root user `harness` (uid 10001). Only the binary
and `agent-registry/` are copied in.

## Run with compose

```sh
docker compose up --build
docker compose exec ollama ollama pull llama3.2
curl -s localhost:8080/healthz
```

`docker-compose.yml` starts the harness plus an `ollama` model server and wires
`OPENAI_BASE_URL` to it. Both services have healthchecks; the harness restarts
unless stopped. The registry is bind-mounted read-only, the SQLite database lives
on a writable `./data` volume.

For a hosted endpoint, remove the `ollama` service and set `OPENAI_BASE_URL` and
`OPENAI_API_KEY` in a `.env` file beside the compose file.

## Configuration

Every knob is an environment variable; no config file is required.

| Variable | Default | Purpose |
|---|---|---|
| `HARNESS_ADDR` | `127.0.0.1:8080` | listen address (image sets `0.0.0.0:8080`) |
| `HARNESS_REGISTRY` | `./agent-registry` | registry root |
| `HARNESS_DB` | `./data/harness.db` | SQLite path (jobs + step results) |
| `HARNESS_STORE` | `sqlite` | job store backend: `sqlite` or `redis` |
| `HARNESS_REDIS_ADDR` | `127.0.0.1:6379` | Redis address when `HARNESS_STORE=redis` |
| `HARNESS_REDIS_PASSWORD` | unset | Redis `AUTH` password |
| `HARNESS_REDIS_DB` | `0` | Redis logical database (`SELECT`) |
| `HARNESS_REDIS_PREFIX` | `jh:` | key namespace (run several deployments on one Redis) |
| `HARNESS_WORKERS` | number of CPUs | worker goroutines |
| `HARNESS_RETRIES` | `3` | max LLM attempts per call (`1` disables) |
| `HARNESS_AUTH_TOKEN` | unset | bearer token for `/v1/*` (see below) |
| `ENABLE_TOOLS` | `false` | opt in to built-in function calling |
| `OPENAI_BASE_URL` | `http://127.0.0.1:11434/v1` | OpenAI-compatible endpoint |
| `OPENAI_MODEL` | unset | default model when a blueprint omits one |
| `OPENAI_API_KEY` | unset | bearer sent to the endpoint |

Health endpoints `/healthz`, `/readyz`, and the metrics endpoint `/metrics` are
always unauthenticated. Everything under `/v1/` requires
`Authorization: Bearer $HARNESS_AUTH_TOKEN` when the token is set. Bind to
`127.0.0.1` unless you have set the token and put a reverse proxy in front.

## GitOps: the registry is the deployment

Agent behavior is data, not code. A change to an agent or pipeline is a pull
request against `agent-registry/`, reviewed like any other change.

1. Agents live in `agent-registry/blueprints/*.json` with their prompts in
   `agent-registry/prompts/*.md`; pipelines in `agent-registry/pipelines/*.json`;
   output schemas in `agent-registry/schemas/*.json`.
2. Every file is validated on load and on every write through the registry CRUD
   API. A malformed entry fails startup (or the HTTP write) with a clear error, so
   a bad merge is caught before it serves traffic.
3. Promote by tagging the source and building your image from that tag. Pass the
   tag through the compose build arg `VERSION` (or `docker build --build-arg
   VERSION=0.1.0`) so `harness --version` and `/healthz` identify the build. No
   container registry is published by CI; build and push the image in your own
   pipeline if you need one.

Two ways to roll the registry forward:

- **Baked at build time.** The registry is copied into the image. Roll a new
  image and redeploy. Simplest and immutable.
- **Mounted from git.** Bind-mount a checkout of `agent-registry/` (as compose
  does). Update the checkout and restart, or use the registry CRUD API to write
  files in place, which hot-swaps the in-memory snapshot without a restart. Mount
  it read-only if you only deploy through new images.

## Job store: SQLite or Redis

Job state lives in embedded SQLite by default: one file, no external service,
ideal for a single instance. Set `HARNESS_STORE=redis` to move it to Redis
instead, which buys two things:

- **Restart durability independent of the container filesystem.** State survives
  a redeploy even when `./data` is not a volume.
- **Shared state across replicas.** Several harness processes can point at the
  same Redis (use a distinct `HARNESS_REDIS_PREFIX` per deployment).

Redis keys are ordinary data under the prefix: `seq`, `sessions`, `job:<id>`
hashes, `jobs:<STATUS>` sorted sets, and `steps:<id>` lists. Nothing else is
stored. The adapter speaks the wire protocol directly (no extra dependency) and
reuses a small connection pool.

```bash
docker run -d --name redis -p 127.0.0.1:6379:6379 redis:7-alpine
HARNESS_STORE=redis HARNESS_REDIS_ADDR=127.0.0.1:6379 ./harness
```

SQLite remains the default and is the right choice unless you need shared or
restart-durable state.

## Operating

- **State.** Jobs and step results live in one SQLite file (WAL mode). Back it up
  by copying `harness.db` (plus `-wal`/`-shm`) while the process is stopped, or
  use `sqlite3 harness.db ".backup ..."` online. On startup, jobs left `RUNNING`
  by a crash are requeued automatically.
- **Capacity.** Workers default to the CPU count. Local CPU inference serializes,
  so keep the pool small; raise it only when the endpoint is a hosted provider or
  a GPU box.
- **Observability.** Scrape `GET /metrics` (Prometheus text). Counters include
  jobs submitted/completed/failed, LLM retries, and schema-validation failures.
- **Logs.** One line per request (`METHOD path status duration`) and per job
  (id, kind, status, duration). The binary logs to stderr.

## Hardening checklist before exposing beyond localhost

- [ ] Set `HARNESS_AUTH_TOKEN` to a strong random value.
- [ ] Keep `ENABLE_TOOLS=false` unless agents genuinely need function calling.
- [ ] Terminate TLS at a reverse proxy; the harness speaks plain HTTP.
- [ ] Keep `/healthz`, `/readyz`, and `/metrics` on your internal network.
- [ ] Back up `HARNESS_DB`; treat the registry repo as the source of truth.
