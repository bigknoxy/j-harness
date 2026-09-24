# j-harness

A lightweight, configuration-driven LLM agent harness in Go.

Agents are defined by **Markdown prompts** (`.md`) and **JSON blueprints/pipelines**. The
harness loads them, runs them against any **OpenAI-compatible endpoint** (OpenAI, Ollama,
vLLM, llama.cpp server), and exposes an async HTTP API: submit a job, poll for the result.

Single binary. Embedded SQLite. No external services required.

> **Status: v1 (Phase 5).** Single agents and sequential pipelines run end to end over an
> async API (`POST .../execute` → `202 {session_id}`, then `GET /v1/sessions/{id}`). Branching
> routers, registry CRUD, and tools arrive in later phases. See
> [`tasks/roadmap.md`](tasks/roadmap.md) for live progress and [`tasks/todo.md`](tasks/todo.md)
> for the current work item.

## Why

- **Prompts are data, not code.** Behavior lives in git-tracked `.md`/`.json` files, so
  non-developers can tune agents without touching Go.
- **Composable.** Simple single agents today; stack them into DAG pipelines (with branching
  routers) later — same schema, no engine rewrite.
- **Local-first.** Point it at local CPU models (Ollama/llama.cpp) or hosted OpenAI. One
  config field switches the endpoint.
- **Runs anywhere.** One Go binary + SQLite. No Redis / message broker required to start.

## Quickstart

```bash
make build            # -> bin/harness
export OPENAI_API_KEY=sk-...            # or leave unset for a local endpoint
bin/harness --addr 127.0.0.1:8080
curl -s localhost:8080/healthz          # {"status":"ok","version":"..."}
```

Docker:

```bash
docker compose up --build               # starts harness + ollama
```

## How it works

```
agent-registry/
  blueprints/*.json   # which prompt, model, params, tools
  prompts/*.md        # system prompt / persona
  pipelines/*.json    # DAG of steps: agent steps + router steps
```

At runtime each pipeline step resolves its input via a strict template grammar
(`{{ inputs.x }}`, `{{ steps.<id>.output }}`), calls the model, and stores a **named**
output that later steps can reference. See [`docs/SCHEMA.md`](docs/SCHEMA.md).

## HTTP API

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/healthz` | liveness |
| `GET`  | `/readyz` | readiness |
| `POST` | `/v1/agents/{id}/execute` | submit one agent run → `202 {session_id}` |
| `POST` | `/v1/pipelines/{id}/execute` | submit a pipeline run → `202 {session_id}` |
| `GET`  | `/v1/sessions/{id}` | poll job status + result |
| `GET`  | `/v1/sessions/{id}/steps` | per-step results |

```bash
# submit
SESSION=$(curl -s -X POST localhost:8080/v1/agents/generic_agent/execute \
  -H 'Content-Type: application/json' \
  -d '{"input_data":"Say hello in five words."}' | jq -r .session_id)

# poll until COMPLETED
curl -s localhost:8080/v1/sessions/$SESSION
```

Registry CRUD and tools arrive in later phases. Full reference:
[`docs/API.md`](docs/API.md).

## Security

This service can execute LLM-driven tool calls. **Treat it as remote code execution.**

- Default bind is `127.0.0.1`; when exposed, set `HARNESS_AUTH_TOKEN` and require a bearer token.
- Tool calling is **off by default** (`ENABLE_TOOLS=false`) and restricted to a per-deployment
  allowlist when enabled.
- Never commit secrets. `OPENAI_API_KEY` is read from the environment only and is never logged.

## Roadmap

| Phase | Deliverable |
|---|---|
| 0 | bootstrap: repo, CI, docs, health endpoint |
| 1 | registry: load/validate/atomic-write blueprints + prompts |
| 2 | llm client + single-agent execution |
| 3 | sync HTTP API |
| 4 | SQLite store + async jobs + worker pool |
| 5 | sequential pipeline (named IO) — **v1** |
| 6 | DAG fan-out/fan-in + router |
| 7 | registry CRUD API |
| 8 | tools / function calling (gated) |
| 9 | hardening: retries, schema validation, metrics |
| 10 | Docker + GitOps deploy docs |
| 11 | optional Redis store adapter |

## Documentation

- [`AGENTS.md`](AGENTS.md) — start here if you are an agent picking up this repo cold.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — components & decisions.
- [`docs/SCHEMA.md`](docs/SCHEMA.md) — blueprint/pipeline schema.
- [`docs/API.md`](docs/API.md) — HTTP API reference.
- [`docs/MEMORY.md`](docs/MEMORY.md) — decision log.

## License

MIT (see `LICENSE`).
