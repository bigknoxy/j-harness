# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 2 — LLM client + single-agent execution**
  - [ ] `internal/llm`: `Client` interface (`Complete(ctx, Request) (Response, error)`)
  - [ ] OpenAI-compatible `http` impl (chat completions `/v1/chat/completions`), env key,
        per-agent `base_url`/`model`/temperature/max_tokens/timeout
  - [ ] `fake` client for tests
  - [ ] `internal/engine`: single-agent run (blueprint + prompt → request → response),
        `output_format=json` handling
  - [ ] Unit tests with an `httptest` server

## Next action

Define the `llm.Client` interface and OpenAI-compatible implementation, then wire a
single-agent `engine.RunAgent` behind tests.

## Blockers

None.

## Done

- [x] **Phase 0 — Bootstrap** (CI + Pages green; Pages live at
  https://bigknoxy.github.io/j-harness/)
- [x] **Phase 1 — Registry** (verified: `make fmt-check vet test build` passed; smoke
  loads `2 blueprint(s), 1 pipeline(s)`; tests cover load/validate/atomic-write, unknown
  fields, bad id, filename mismatch, missing prompt, traversal, bad version, forward refs,
  unknown input, and the real checked-in registry)
  - `internal/model`: `AgentBlueprint`, `Pipeline`, `Step`, `Route`, `Condition`, `Job`,
    `StepResult`, operators + `SchemaVersion`
  - `internal/pipeline/template.go`: strict non-evaluating ref parser
  - `internal/registry`: `Load` + `WriteBlueprint`/`WritePipeline` (atomic temp+rename),
    strict JSON (no unknown fields, no trailing data), path-traversal guard

## Notes / working memory

- Target v1 = end of Phase 5 (sequential pipeline). Router arrives Phase 6.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes).
- `{{ steps.<id>.output }}` keys off the **step id**; router `goto` may point forward.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
