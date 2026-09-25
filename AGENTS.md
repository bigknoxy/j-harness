# AGENTS.md: start here

This file is the entrypoint for **any agent (or human) resuming this repo with no memory**.
Read it top to bottom, then follow the pointers in section 2.

## 1. What this repo is

`j-harness`: a lightweight, config-driven LLM agent harness in Go. Agents are defined by
`.md` prompts + `.json` blueprints; pipelines are `.json` DAGs. Runtime is a single Go binary
with embedded SQLite (optional Redis via `HARNESS_STORE=redis`). Targets any
OpenAI-compatible endpoint.

## 2. Where we are right now

- **Current phase:** all engine phases 0-11 are complete; see
  [`tasks/roadmap.md`](tasks/roadmap.md).
- **Current work item / next action / blockers:** see [`tasks/todo.md`](tasks/todo.md).
- **Decisions made and why:** see [`docs/MEMORY.md`](docs/MEMORY.md).
- **Mistakes to avoid:** see [`tasks/lessons.md`](tasks/lessons.md).
- **Doc index / which change updates which doc:** see [`docs/DOCUMENTATION.md`](docs/DOCUMENTATION.md).

Resume rule: pick up the single `in_progress` item in `tasks/todo.md`. Do one thing, verify
it, then update `tasks/todo.md` before starting the next.

## 3. Commands

```bash
make build        # build bin/harness
make test         # go test -trimpath -race ./...
make lint         # go vet + gofmt check
make run          # run locally
make docker       # build image
```

Full gate before claiming a task done:

```bash
make fmt-check vet test build
```

## 4. Conventions (non-negotiable)

- Follow the layout in `docs/ARCHITECTURE.md`. Put new code in the matching `internal/` package.
- **No new dependencies** without a decision entry in `docs/MEMORY.md` explaining why the
  standard library / existing stack can't do it.
- Every phase-advancing change must: (1) pass `make fmt-check vet test build`,
  (2) update `tasks/todo.md` + `tasks/roadmap.md`, (3) add a `docs/MEMORY.md` line for any
  architectural decision, (4) update `README.md` if the public API changed.
- Secrets are env-only. Never commit keys. Never log `OPENAI_API_KEY`.
- Tool execution is gated by `ENABLE_TOOLS`; do not add an unauthenticated execution path.

## 5. Definition of done

Behavior matches the phase acceptance criteria, the full gate passes, docs are updated, and
`tasks/todo.md` records what changed + how it was verified.
