# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 10 — Docker + compose + GitOps deploy docs**

## Next action

- Confirm the checked-in `Dockerfile` + `docker-compose.yml` cover harness + ollama and that
  the image is `CGO_ENABLED=0`. Then document a GitOps deploy (registry in git, image tag
  pinned, `ENABLE_TOOLS`/`HARNESS_RETRIES` as config) and verify `docker build` locally.

## Blockers

None.

## Backlog (engine phases, in order)

- [ ] **Phase 10**: Docker + compose + GitOps deploy docs
- [ ] **Phase 11**: optional Redis `Store` adapter

## Done

- [x] **Phase 9 — Hardening (retries / backoff, output-schema validation, metrics)**
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

- [x] **Phase 8 — Tools / function calling (gated)** (verified: `go build ./...` clean;
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

- [x] **Phase 7 — Registry CRUD API** (verified: `make fmt-check vet test build` passed;
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

- [x] **Phase 6 — Parallel DAG fan-out/fan-in** (verified: `make fmt-check vet test build`
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
- [x] **D8 — Dogfood: real runs via a hosted OpenAI-compatible provider**
  (verified: ran the checked-in registry through the async API against NVIDIA NIM
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. `triage` classified billing -> high and a
  crash -> technical; `support_flow` routed correctly to `billing_reply` / `tech_reply` with
  the other branches `SKIPPED`. Homelab Ollama was saturated, so it was not used. Notes in
  `docs/MEMORY.md`.)
- [x] **Delivery track D1–D7** (branch protection, CI, GoReleaser releases, installer +
  uninstaller, Pages site, repo polish, README badges). v0.1.0 release published with
  linux/darwin/windows amd64+arm64 assets; one-liner install/uninstall verified end to end.
- [x] **Phase 0 — Bootstrap** (CI + Pages green; live at https://bigknoxy.github.io/j-harness/)
- [x] **Phase 1 — Registry** (load/validate/atomic-write + tests)
- [x] **Phase 2 — LLM client + single-agent execution**
- [x] **Phase 3 — HTTP API (sync) + middleware**
- [x] **Phase 4 — SQLite store + async jobs + bounded worker pool + orphan requeue**
  (verified: `make fmt-check vet test build` passed; smoke green; store round-trip,
  worker lifecycle/requeue/queue-full, async API success/failure/auth/404/405 covered.
  Go toolchain/Dockerfile/CI now track current stable 1.27; pure-Go `modernc.org/sqlite`
  behind a `Store` interface. See `docs/MEMORY.md`.)
- [x] **Phase 5 — Sequential pipeline + router (v1)** (verified: `make fmt-check vet test build`
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

- v1 = Phase 5 (sequential pipeline); Phase 6 adds DAG fan-out/fan-in.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes); DAG fan-out is
  bounded by `GOMAXPROCS`.
- Step dependencies come from `{{ steps.<id>.output }}` refs + explicit `needs` + router
  gotos; cycles are rejected at load. `{{ steps.<id>.output }}` keys off the **step id**.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Dogfood backends: Ollama `http://192.168.8.136:11434` (`qwen3:8b`); NVIDIA NIM
  `https://integrate.api.nvidia.com/v1` (key in `/root/.bashrc`), working model
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. Do not overload the homelab.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
