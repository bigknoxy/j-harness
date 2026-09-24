# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] **Phase 0 — Bootstrap**
  - [x] `git init -b main`, directory skeleton
  - [x] `go.mod`, `.gitignore`, `Makefile`, `cmd/harness/main.go` (healthz stub)
  - [x] `README.md`, `AGENTS.md`, `tasks/`, `docs/` scaffolding
  - [ ] `Dockerfile`, `docker-compose.yml`, `config.example.json`, `scripts/`
  - [ ] CI workflow, gh-pages workflow, `.nojekyll`
  - [ ] Verify: `make fmt-check vet test build`
  - [ ] Commit, `gh repo create bigknoxy/j-harness --public`, push, enable Pages

## Next action

Finish the remaining Phase 0 files, run the verification gate, then create + push the repo.

## Blockers

None.

## Notes / working memory

- Target v1 = end of Phase 5 (sequential pipeline). Router arrives Phase 6.
- State = embedded SQLite behind a `Store` interface (Redis is a later adapter, Phase 11).
- Worker pool is **bounded** (2-core host; local inference serializes).
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
