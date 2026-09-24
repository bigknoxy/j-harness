# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 7 — Registry CRUD API**
  - [ ] `POST`/`PUT /v1/registry/agents/{id}` (blueprint + prompt body)
  - [ ] `POST`/`PUT /v1/registry/pipelines/{id}`
  - [ ] `GET /v1/registry/{agents,pipelines}` (list) + `GET .../{id}` (detail)
  - [ ] validate before write; reject invalid with the registry error
  - [ ] live reload after write (rebuild the in-memory registry snapshot)
  - [ ] auth-gated; tests + docs

## Next action

Reuse `registry.WriteBlueprint` / `WritePipeline` (they already validate + atomic-write),
then hot-swap the engine's registry snapshot. Decide the reload strategy (whole-registry
`Load` vs targeted upsert) and record it in `docs/MEMORY.md`.

## Blockers

None.

## Backlog (engine phases, in order)

- [ ] **Phase 8**: tools / function calling (gated)
- [ ] **Phase 9**: hardening (retries, JSON-schema validation, metrics)
- [ ] **Phase 10**: Docker + compose + GitOps deploy docs
- [ ] **Phase 11**: optional Redis `Store` adapter

## Done

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
