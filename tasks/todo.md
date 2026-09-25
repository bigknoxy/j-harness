# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] (none: all phases complete)

## Next action

- No engine phases remain. Optional follow-ups if ever wanted: streaming
  responses, a `POST /v1/sessions/{id}/cancel` endpoint, or per-tenant auth. Not
  scheduled; add a new phase to `tasks/roadmap.md` first if picked up.

## Blockers

None.

## Backlog (engine phases, in order)

- [ ] (empty)

## Done

- [x] **WS4 / R2+R4+R5: docs drift gate enforced on every PR** (verified:
  `go test ./internal/docscheck/...` 6 tests pass; deliberately corrupted README/API
  fixtures each fail with a file-naming diff message (stale route, missing route, stale
  env var, non-ASCII rune); `gofmt -l .` empty; `go vet ./...` clean;
  `go test -race ./...` all packages ok; `make fmt-check vet test` green;
  `make docs` green; CI YAML parsed by `yaml.safe_load`.)
  - `internal/docscheck/docscheck.go`: pure-Go, stdlib-only parsers for source routes
    (mux patterns + route comments), documented routes, env vars, metrics counters,
    version pins, and relative markdown links (with heading anchors), plus a heading
    `Slugify`; no network, no dependency
  - `internal/docscheck/docscheck_test.go`: six deterministic tests - routes in
    README+API (both directions), env vars in API+DEPLOY (both directions), metrics in
    API (both directions), README/DEPLOY version pin agreement, relative-link/anchor
    resolution, ASCII-only docs
  - `.github/workflows/ci.yml`: new always-run, non-path-filtered `docs` job
    (`go test ./internal/docscheck/...`) on push+PR; new `pr-title` Conventional
    Commits job on PR; existing lint/test/vuln/smoke untouched
  - `.github/PULL_REQUEST_TEMPLATE.md`: docs checklist that references the docs map
  - `Makefile`: `make docs` target
  - docs: DOCUMENTATION.md hard rule + automated drift-check section + package row;
    README testing section + local usage; MEMORY.md decision entry (pure Go vs
    lychee/vale/markdownlint; always-run job); roadmap D9

- [x] **R1/R3/R6: real HTTP e2e suite + deterministic eval suite** (verified:
  `gofmt -l .` empty; `go vet ./...` clean; `go test -race ./...` all packages ok
  incl. new `internal/e2e` (7 tests, ~1.4s) and `internal/eval` (9 cases, all
  CORRECT); `make fmt-check vet test` green.)
  - `internal/llmstub`: test-only OpenAI-compatible `httptest` server with scripted
    responses (ordered or selected by system prompt); no network, no model
  - `internal/e2e/e2e_test.go`: full stack over real HTTP (registry -> temp SQLite ->
    engine -> worker pool -> `api.Handler()` -> `httptest.Server`) driving the real
    `llm.NewOpenAI` (+ retry) client at the stub. Covers agent execute -> 202 ->
    poll COMPLETED + result + steps; `support_flow` routing (billing vs technical
    branches COMPLETED vs SKIPPED); `digest_flow` DAG; auth boundary (401 on
    `/v1/*`, 200 on `/healthz`/`/readyz`/`/metrics`); registry CRUD round-trip +
    400 on invalid write; metrics after jobs; 12-job concurrency smoke. Deadline
    polling (3ms interval, 5s deadline), no fixed sleeps.
  - `internal/eval` (`eval_test.go` + `cases.json`): checked-in deterministic eval
    runner with explicit CORRECT/INCORRECT scoring and a summary; cases for triage
    classification, output-schema conformance + single repair turn, fail-closed on
    unrepairable output, `support_flow` routing, and the bounded tool-call loop.
  - `docs/EVALS.md`; `make e2e` / `make eval` targets; docs/ARCHITECTURE testing
    section + package rows; README testing section + doc link; DOCUMENTATION map.

- [x] **Phase 11: Optional Redis `Store` adapter**
  (verified: `go test -race ./...` all packages ok incl. new
  `internal/store/redis` (1.04s); `internal/store/redis/redis_test.go` runs the full
  `Store` round-trip against a real Redis 7 (`durable across restart`, `SetJobStatus`
  moves jobs between per-status sets, `ErrNotFound` on missing, duplicate-create
  rejection); live end-to-end run with `HARNESS_STORE=redis`: submitted a `triage`
  job, polled it to `COMPLETED` with step results, then killed + restarted the
  process and the session was still readable; `make fmt-check vet test build` +
  smoke green.)
  - `internal/store/store.go`: interface unchanged; SQLite stays the default
  - `internal/store/redis`: `RedisStore` implementing `store.Store`, selected via
    `HARNESS_STORE=sqlite|redis`; `HARNESS_REDIS_ADDR`/`HARNESS_REDIS_PASSWORD`/
    `HARNESS_REDIS_DB`/`HARNESS_REDIS_PREFIX` config
  - `internal/store/redis/client.go`: small stdlib-only RESP client with a pooled
    connection, MULTI/EXEC transactions, and context deadlines (no new dependency)
  - `cmd/harness`: `--store` flag + `openStore` backends (`--db` still used for SQLite)
  - docs: README status/roadmap/feature, docs/DEPLOY env table + job-store section,
    docs/ARCHITECTURE package row

- [x] **Phase 10: Docker + compose + GitOps deploy docs**
  (verified: `docker build --build-arg VERSION=0.1.0 -t j-harness:test .` succeeded;
  `docker run --rm j-harness:test --version` -> `j-harness 0.1.0`; booted container served
  `/healthz {"status":"ok","version":"0.1.0"}` and `/metrics` counters; image ~20MB.
  Full gate + smoke green.)
  - `.dockerignore` added so the build context is small and reproducible
  - `Dockerfile`: `ARG VERSION` stamped into `harness --version` via ldflags
  - `docker-compose.yml`: harness + ollama with healthchecks, `depends_on: service_healthy`,
    registry mounted read-only, `./data` volume, pinned-image option, configurable env
  - `docs/DEPLOY.md`: image build, compose, full env table, auth note, GitOps
    "the registry is the deployment" model (promote by tag; baked vs mounted), operating
    notes (SQLite WAL backup, orphan requeue, `/metrics`, logs), hardening checklist

- [x] **Phase 9: Hardening (retries / backoff, output-schema validation, metrics)**
  (verified: `make fmt-check vet test build` passed; smoke green incl. a `GET /metrics`
  assertion; `internal/schema/schema_test.go`, `internal/llm/retry_test.go`,
  `internal/metrics/metrics_test.go`, and extended `internal/engine/engine_test.go`
  repair tests all pass)
  - `internal/schema`: small draft-07 subset validator; registry compiles the referenced
    `output_schema` at load (malformed schema fails startup; requires `output_format: "json"`)
  - `internal/engine`: JSON output validated against the schema with exactly ONE repair turn,
    then fails; increments `harness_schema_failures_total`
  - `internal/llm`: typed `HTTPError` + `NewRetry` decorator; retries transport errors and
    HTTP 429/5xx with exponential backoff + jitter, 4xx fails fast (`HARNESS_RETRIES`, default 3)
  - `internal/metrics`: in-process Prometheus-text counters on unauthenticated `GET /metrics`
  - docs: README/docs/API (health + counters + Environment tables)/docs/ARCHITECTURE
    (`## Hardening`)/docs/SCHEMA; tasks/lessons.md nil-receiver lesson

- [x] **Phase 8: Tools / function calling (gated)** (verified: `go build ./...` clean;
  `gofmt -l internal cmd` empty; `go test -race ./...` all packages ok;
  `internal/tools/tools_test.go` covers the arithmetic evaluator + registry;
  `internal/engine/tools_test.go` covers tool round-trip, fail-closed when disabled,
  unknown-tool rejection, no-tools passthrough, and the bounded loop)
  - `internal/tools`: fixed built-in allowlist (`current_time`, `word_count`, `math_eval`);
    pure, side-effect-free, no shell/fs/network; own recursive-descent arithmetic evaluator
  - `internal/llm`: `Message` gained `ToolCalls`/`ToolCallID`; `Request` gained `Messages`
    + `Tools`; `Response` gained `ToolCalls`; OpenAI client forwards tools
  - `internal/engine/tools.go`: `resolveTools` (fail-closed) + `completeWithTools`
    (bounded `maxToolRounds = 5`)
  - `internal/registry`: unknown tool names rejected at load
  - `cmd/harness`: `ENABLE_TOOLS=true` builds the allowlist and calls `eng.SetTools`
  - `agent-registry/blueprints/calculator.json` + prompt: checked-in tool example

- [x] **Phase 7: Registry CRUD API** (verified: `make fmt-check vet test build` passed;
  smoke green; `internal/api/registry_test.go` covers list/get/create/update, id-mismatch
  and invalid payload rejection, unknown-field rejection, 409 on duplicate create, 404 on
  PUT-missing, pipeline CRUD, 405 + Allow, and auth-gating. `internal/engine/registry_swap_test.go`
  covers snapshot swap + nil no-op.)
  - `internal/api/registry.go`: `GET|POST|PUT /v1/registry/{agents,pipelines}[/{id}]`;
    strict JSON bodies; create=409 on existing, update=404 on missing
  - `internal/api/api.go`: writes go through `mutate` -> validate -> atomic write ->
    whole-registry `Load` -> hot-swap API + engine snapshots (serialized by `mu`)
  - `internal/engine/engine.go`: registry snapshot behind `sync.RWMutex` + `SetRegistry`;
    `RunAgent`/`RunPipeline` read via `getRegistry` (in-flight runs keep their snapshot)
  - docs/API.md: registry management section + `409` status code

- [x] **Phase 6: Parallel DAG fan-out/fan-in** (verified: `make fmt-check vet test build`
  passed; smoke green; `internal/pipeline/graph_test.go` covers refs/needs/router edges,
  cycles, unknown refs. `internal/engine/dag_test.go` proves concurrent fan-out with a
  barrier client and stops on failure. Registry tests cover forward-ref-allowed + cycle reject.)
  - `internal/pipeline/graph.go`: `Deps` unions template refs + `needs` + router gotos, rejects
    unknown edges and cycles at load time
  - `internal/model`: `Step.Needs` + `StatusSkipped`
  - `internal/engine/pipeline.go`: concurrent DAG scheduler bounded by `GOMAXPROCS`; failed or
    skipped predecessors skip their dependents
  - `internal/registry`: forward refs allowed; cycles/unknown needs rejected
  - `agent-registry/pipelines/digest_flow.json`: fan-out (`summarize` + `assess_risk`) -> join
    (`combine` via `needs`)
- [x] **D8: Dogfood: real runs via a hosted OpenAI-compatible provider**
  (verified: ran the checked-in registry through the async API against NVIDIA NIM
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. `triage` classified billing -> high and a
  crash -> technical; `support_flow` routed correctly to `billing_reply` / `tech_reply` with
  the other branches `SKIPPED`. Homelab Ollama was saturated, so it was not used. Notes in
  `docs/MEMORY.md`.)
- [x] **Delivery track D1-D7** (branch protection, CI, GoReleaser releases, installer +
  uninstaller, Pages site, repo polish, README badges). v0.1.0 release published with
  linux/darwin/windows amd64+arm64 assets; one-liner install/uninstall verified end to end.
- [x] **Phase 0: Bootstrap** (CI + Pages green; live at https://bigknoxy.github.io/j-harness/)
- [x] **Phase 1: Registry** (load/validate/atomic-write + tests)
- [x] **Phase 2: LLM client + single-agent execution**
- [x] **Phase 3: HTTP API (sync) + middleware**
- [x] **Phase 4: SQLite store + async jobs + bounded worker pool + orphan requeue**
  (verified: `make fmt-check vet test build` passed; smoke green; store round-trip,
  worker lifecycle/requeue/queue-full, async API success/failure/auth/404/405 covered.
  Go toolchain/Dockerfile/CI now track current stable 1.27; pure-Go `modernc.org/sqlite`
  behind a `Store` interface. See `docs/MEMORY.md`.)
- [x] **Phase 5: Sequential pipeline + router (v1)** (verified: `make fmt-check vet test build`
  passed; smoke green; resolver/condition/router tests, engine pipeline tests, worker pipeline
  test, API pipeline submit + steps tests)
  - `internal/pipeline/resolve.go`: runtime `Resolve(scope)` for `{{ inputs.x }}` and
    `{{ steps.<id>.output[.json.path] }}` (JSON path incl. array indices)
  - `internal/pipeline/condition.go`: `EvaluateCondition` + `PickRoute` (equals/not_equals/
    in/matches/exists) with default fallback
  - `internal/engine/pipeline.go`: `RunPipeline` walks steps in order, router selects one
    successor and marks others `SKIPPED`, result = last agent step when `output` is omitted
  - `internal/engine/worker.go`: `KindPipeline` jobs execute + persist every step outcome
  - `internal/api`: `POST /v1/pipelines/{id}/execute` -> `202`, strict named-input body
  - `agent-registry/pipelines/support_flow.json`: top-level `output` removed (branch-safe)

## Notes / working memory

- v1 = Phase 5 (sequential pipeline); Phase 6 added DAG fan-out/fan-in.
- State = embedded SQLite behind a `Store` interface by default; Redis is an opt-in
  adapter (`HARNESS_STORE=redis`) shipped in Phase 11.
- Worker pool is **bounded** (2-core host; local inference serializes); DAG fan-out is
  bounded by `GOMAXPROCS`.
- Step dependencies come from `{{ steps.<id>.output }}` refs + explicit `needs` + router
  gotos; cycles are rejected at load. `{{ steps.<id>.output }}` keys off the **step id**.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Dogfood backends: Ollama `http://192.168.8.136:11434` (`qwen3:8b`); NVIDIA NIM
  `https://integrate.api.nvidia.com/v1` (key in `/root/.bashrc`), working model
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. Do not overload the homelab.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
