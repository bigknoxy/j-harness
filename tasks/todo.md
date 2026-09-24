# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 4 — SQLite store + async jobs + bounded worker pool + orphan requeue**
  - [ ] `internal/store`: `Store` interface + SQLite (WAL) implementation (jobs + step results)
  - [ ] job state machine: `PENDING→RUNNING→COMPLETED|FAILED|CANCELED`
  - [ ] bounded in-process worker pool (default = CPU count)
  - [ ] startup requeue of orphaned `RUNNING` jobs
  - [ ] async API: `POST .../execute` → `202 {session_id}`; `GET /v1/sessions/{id}`
  - [ ] `GET /readyz` reflects store readiness
  - [ ] tests (store round-trip, requeue, async handler)

## Next action

Define the `Store` interface (create/get/update job, append step result, list orphaned),
implement the SQLite adapter with WAL, then wire a bounded worker pool that drains the queue.

## Blockers

None.

## Done

- [x] **Phase 0 — Bootstrap** (CI + Pages green; live at https://bigknoxy.github.io/j-harness/)
- [x] **Phase 1 — Registry** (load/validate/atomic-write + tests)
- [x] **Phase 2 — LLM client + single-agent execution** (verified: `make fmt-check vet test build`
  passed; smoke green; tests cover OpenAI request/response, json_object format, error status,
  fake ordering, `RunAgent` text/JSON-valid/JSON-invalid/unknown/client-error, code-fence strip)
  - `internal/llm`: `Client` interface, `OpenAIClient` (chat completions, env key,
    per-request base_url/model/timeout/json_object), `Fake`
  - `internal/engine`: `New`, `RunAgent` (blueprint+prompt → request → response, JSON validation)
  - `main` wires registry + env-configured client + engine
- [x] **Phase 3 — HTTP API (sync) + middleware** (verified: `make fmt-check vet test build`
  passed; smoke green; `internal/api` tests cover success, unknown agent, bad bodies,
  405 `Allow`, unknown routes, client error → 500, bearer auth (missing/wrong/valid),
  unauthenticated `/healthz`/`/readyz`, panic recovery)
  - `internal/api`: `New(Config)` + `Handler()`; `POST /v1/agents/{id}/execute` (sync,
    200 `{agent_id,output,tokens,duration_ms}`), `GET /healthz`, `GET /readyz`
  - middleware chain: recoverer → request logging → bearer auth (only `/v1/*` when
    `HARNESS_AUTH_TOKEN` set) → mux
  - JSON error envelope `{"error":{"code,message}}`; strict body decoding (1 MiB cap,
    unknown fields rejected, single JSON object, `input_data` required)
  - `main` now serves `handler.Handler()` instead of an inline mux

## Notes / working memory

- Target v1 = end of Phase 5 (sequential pipeline). Router arrives Phase 6.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes).
- `{{ steps.<id>.output }}` keys off the **step id**; router `goto` may point forward.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
