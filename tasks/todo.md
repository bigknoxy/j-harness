# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 1 — Registry (load / validate / atomic write)**
  - [ ] `internal/model`: `AgentBlueprint`, `Pipeline`, `Step`, `Job`, `StepResult` types
  - [ ] `internal/registry`: load blueprints/prompts/pipelines; validate `version`, IDs,
        prompt paths, template refs, router conditions
  - [ ] Atomic file writes (temp + rename); ID sanitization `[a-z0-9_-]+`; path-traversal guard
  - [ ] Unit tests: happy path, missing file, unknown field, bad ID, traversal attempt

## Next action

Define `internal/model` types, then implement `internal/registry` with validation and tests.

## Blockers

None.

## Done

- [x] **Phase 0 — Bootstrap** (verified: `make fmt-check vet test build` passed;
  `scripts/smoke.sh` returned `{"status":"ok"}`; CI + Pages workflows green;
  repo public at https://github.com/bigknoxy/j-harness; Pages live at
  https://bigknoxy.github.io/j-harness/)
  - git repo, layout, go.mod, Makefile, Dockerfile, compose, config example
  - health endpoint stub + graceful shutdown, smoke script
  - CI + gh-pages workflows, README, AGENTS, docs, tasks

## Notes / working memory

- Target v1 = end of Phase 5 (sequential pipeline). Router arrives Phase 6.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes).
- Pages gotcha: no `.nojekyll` (it breaks README rendering); see `tasks/lessons.md`.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
