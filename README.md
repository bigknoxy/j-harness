# j-harness

[![CI](https://github.com/bigknoxy/j-harness/actions/workflows/ci.yml/badge.svg)](https://github.com/bigknoxy/j-harness/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/bigknoxy/j-harness?color=34d399)](https://github.com/bigknoxy/j-harness/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/bigknoxy/j-harness?color=34d399)](go.mod)
[![License: MIT](https://img.shields.io/github/license/bigknoxy/j-harness?color=34d399)](LICENSE)
[![Docs](https://img.shields.io/badge/site-bigknoxy.github.io%2Fj--harness-34d399)](https://bigknoxy.github.io/j-harness/)

A lightweight, configuration-driven LLM agent harness in Go.

Agents are defined by **Markdown prompts** (`.md`) and **JSON blueprints/pipelines**. The
harness loads them, runs them against any **OpenAI-compatible endpoint** (OpenAI, Ollama,
vLLM, llama.cpp server), and exposes an async HTTP API: submit a job, poll for the result.

Single binary. Embedded SQLite. No external services required.

> **Status: Phase 10.** Single agents and pipelines (including parallel fan-out/fan-in DAGs) run end to end over an
> async API (`POST .../execute` -> `202 {session_id}`, then `GET /v1/sessions/{id}`), the
> registry can be created and updated over HTTP, and agents can call a small set of built-in
> tools when `ENABLE_TOOLS=true`.
> LLM calls retry transient failures with backoff, `json` outputs are validated against their
> referenced JSON Schema (with one repair turn), and Prometheus metrics are exposed on
> `GET /metrics`. A hardened container image and a compose file (harness + ollama) ship with a
> GitOps deploy guide in [`docs/DEPLOY.md`](docs/DEPLOY.md). See
> [`tasks/roadmap.md`](tasks/roadmap.md) for live progress and
> [`tasks/todo.md`](tasks/todo.md) for the current work item.

## Install

One line (Linux / macOS):

```bash
curl -fsSL https://raw.githubusercontent.com/bigknoxy/j-harness/main/install.sh | sh
```

This drops the `harness` binary in `~/.local/bin` and a starter registry in
`~/.config/j-harness/registry`. Pin a version with `VERSION=v1.0.0`, change the location
with `PREFIX=/usr/local`.

Uninstall:

```bash
curl -fsSL https://raw.githubusercontent.com/bigknoxy/j-harness/main/uninstall.sh | sh
```

Prebuilt binaries for Linux, macOS, and Windows (amd64/arm64) are on the
[releases page](https://github.com/bigknoxy/j-harness/releases/latest). Or build from source:

```bash
make build            # -> bin/harness
```

## Why

- **Prompts are data, not code.** Behavior lives in git-tracked `.md`/`.json` files, so
  non-developers can tune agents without touching Go.
- **Composable.** Simple single agents today; stack them into DAG pipelines (with branching
  routers) later. Same schema, no engine rewrite.
- **Local-first.** Point it at local CPU models (Ollama/llama.cpp) or hosted OpenAI. One
  config field switches the endpoint.
- **Runs anywhere.** One Go binary + SQLite. No Redis / message broker required to start.

## Quickstart

```bash
export OPENAI_API_KEY=sk-...            # or leave unset for a local endpoint
bin/harness --addr 127.0.0.1:8080
curl -s localhost:8080/healthz          # {"status":"ok","version":"..."}
```

Docker:

```bash
docker compose up --build               # starts harness + ollama
```

See [`docs/DEPLOY.md`](docs/DEPLOY.md) for image builds, configuration, and the GitOps
deploy model (the registry itself is the deployment, promoted by tag).

## How it works

```
agent-registry/
  blueprints/*.json   # which prompt, model, params, tools
  prompts/*.md        # system prompt / persona
  pipelines/*.json    # DAG of steps: agent steps + router steps
  schemas/*.json      # optional JSON Schemas referenced by blueprints
```

At runtime each pipeline step resolves its input via a strict template grammar
(`{{ inputs.x }}`, `{{ steps.<id>.output }}`), calls the model, and stores a **named**
output that later steps can reference. See [`docs/SCHEMA.md`](docs/SCHEMA.md).

## HTTP API

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/healthz` | liveness |
| `GET`  | `/readyz` | readiness |
| `POST` | `/v1/agents/{id}/execute` | submit one agent run -> `202 {session_id}` |
| `POST` | `/v1/pipelines/{id}/execute` | submit a pipeline run -> `202 {session_id}` |
| `GET`  | `/v1/sessions/{id}` | poll job status + result |
| `GET`  | `/v1/sessions/{id}/steps` | per-step results |
| `GET`  | `/v1/registry/agents` | list agents |
| `POST` | `/v1/registry/agents/{id}` | create or update an agent |
| `GET`  | `/v1/registry/pipelines` | list pipelines |
| `POST` | `/v1/registry/pipelines/{id}` | create or update a pipeline |

```bash
# submit
SESSION=$(curl -s -X POST localhost:8080/v1/agents/generic_agent/execute \
  -H 'Content-Type: application/json' \
  -d '{"input_data":"Say hello in five words."}' | jq -r .session_id)

# poll until COMPLETED
curl -s localhost:8080/v1/sessions/$SESSION
```

Agents and pipelines are edited over HTTP as validated, atomic writes to the
same registry files. Tools arrive in a later phase. Full reference:
[`docs/API.md`](docs/API.md).

## Security

This service runs LLM-driven tool calls, so treat it as code you did not write.

- Default bind is `127.0.0.1`; when exposed, set `HARNESS_AUTH_TOKEN` and require a bearer token.
- Tool calling is **off by default** (`ENABLE_TOOLS=false`). When enabled, only a fixed,
  side-effect-free allowlist is available (`current_time`, `word_count`, `math_eval`); there
  is no shell, filesystem, or arbitrary-network tool. A blueprint that requests an unknown
  tool, or any tool while tools are disabled, fails closed.
- Never commit secrets. `OPENAI_API_KEY` is read from the environment only and is never logged.

## Roadmap

| Phase | Deliverable | Status |
|---|---|---|
| 0 | bootstrap: repo, CI, docs, health endpoint | done |
| 1 | registry: load/validate/atomic-write blueprints + prompts | done |
| 2 | llm client + single-agent execution | done |
| 3 | HTTP API + middleware | done |
| 4 | SQLite store + async jobs + worker pool | done |
| 5 | sequential pipeline (named IO) - **v1** | done |
| 6 | DAG fan-out/fan-in + router | done |
| 7 | registry CRUD API | done |
| 8 | tools / function calling (gated) | done |
| 9 | hardening: retries, schema validation, metrics | done |
| 10 | Docker + GitOps deploy docs | todo |
| 11 | optional Redis store adapter | todo |

## Documentation

- [`AGENTS.md`](AGENTS.md) - start here if you are an agent picking up this repo cold.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) - components and decisions.
- [`docs/SCHEMA.md`](docs/SCHEMA.md) - blueprint/pipeline schema.
- [`docs/API.md`](docs/API.md) - HTTP API reference.
- [`docs/MEMORY.md`](docs/MEMORY.md) - decision log.

## License

MIT (see [`LICENSE`](LICENSE)).
