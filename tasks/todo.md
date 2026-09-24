# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 3 — HTTP API (sync) + middleware**
  - [ ] `internal/api`: `POST /v1/agents/{id}/execute` (sync), `GET /readyz`
  - [ ] middleware: request logging, panic recovery, bearer auth (`HARNESS_AUTH_TOKEN`)
  - [ ] JSON error envelope + status codes (400/401/404/405/500)
  - [ ] `httptest`-based handler tests

## Next action

Build the sync execute handler over `engine.RunAgent`, then add auth/logging/recovery
middleware and tests.

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

## Notes / working memory

- Target v1 = end of Phase 5 (sequential pipeline). Router arrives Phase 6.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes).
- `{{ steps.<id>.output }}` keys off the **step id**; router `goto` may point forward.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
