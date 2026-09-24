# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 6 — Parallel DAG (fan-out/fan-in)**
  - [ ] dependency-aware scheduler: a step's predecessors must all finish first
  - [ ] run independent branches concurrently (bounded by the worker pool)
  - [ ] join step: declared predecessors gate execution; merge their outputs
  - [ ] validation: reject cycles at load time
  - [ ] tests + docs

## Next action

Add explicit step dependencies (`needs: [ids]`) to the schema, or derive them from
template refs. Deriving from refs keeps blueprints terse; explicit `needs` is clearer for
joins. Decide and record in `docs/MEMORY.md`.

## Blockers

None.

## Backlog (delivery track — after engine phases or interleaved)

- [ ] **D5 Pages site**: custom `index.html` (fun, tech-forward, no AI tells)
- [ ] **D6 repo polish**: homepage URL -> Pages, topics, description
- [ ] **D7 README badges**: CI, latest release, Go version, license
- [ ] **D1 branch protection**: `main` requires PR + status checks; admin bypass on
- [ ] **D2 CI**: golangci-lint, test matrix, govulncheck, build/attest
- [ ] **D3 releases**: GoReleaser on tag `v*`, publish to GitHub Releases
- [ ] **D4 installer**: `install.sh` (one-liner) + `uninstall.sh`
- [ ] **D8 dogfood**: real Ollama + NVIDIA NIM runs through the harness
- [ ] **Phase 6**: fan-out/fan-in + router primitive
- [ ] **Phase 7**: registry CRUD API
- [ ] **Phase 8**: tools / function calling (gated)
- [ ] **Phase 9**: hardening (retries, JSON-schema validation, metrics)
- [ ] **Phase 10**: Docker + compose + GitOps deploy docs
- [ ] **Phase 11**: optional Redis `Store` adapter

## Done

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

- Target v1 = end of Phase 5 (sequential pipeline). Router arrives Phase 6.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes).
- `{{ steps.<id>.output }}` keys off the **step id**; router `goto` may point forward.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Dogfood backends: Ollama `http://192.168.8.136:11434` (`qwen3:8b`); NVIDIA NIM
  `https://integrate.api.nvidia.com/v1` (key in `/root/.bashrc`), working model
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. Do not overload the homelab.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
