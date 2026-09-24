# Architecture

## Overview

`j-harness` is a single Go binary that loads agent definitions from disk, executes them
against an OpenAI-compatible endpoint, and exposes an async HTTP API. There are no external
services in v1: configuration is git-tracked files and job state is embedded SQLite.

```
                 ┌──────────────────────────────────────────────┐
   HTTP client   │                  api (net/http)              │
   ────────────► │  POST /v1/{agents,pipelines}/{id}/execute    │
                 │  GET  /v1/sessions/{id}                       │
                 └───────────────┬──────────────────────────────┘
                                 │ enqueue job
                                 ▼
                 ┌──────────────────────────────┐     ┌───────────────────┐
                 │        engine                │◄───►│  store (SQLite)   │
                 │  bounded worker pool         │     │ jobs, step_results│
                 │  job lifecycle / requeue     │     └───────────────────┘
                 └───────────┬──────────────────┘
                             │ run step(s)
                             ▼
                 ┌──────────────────────────────┐     ┌───────────────────┐
                 │        pipeline              │────►│  registry (files) │
                 │  DAG, template resolver,     │     │ blueprints/*.json │
                 │  router conditions           │     │ prompts/*.md      │
                 └───────────┬──────────────────┘     │ pipelines/*.json  │
                             │ llm request            └───────────────────┘
                             ▼
                 ┌──────────────────────────────┐
                 │   llm (OpenAI-compatible)    │
                 │   OpenAI / Ollama / vLLM     │
                 └──────────────────────────────┘
```

## Packages

| Package | Responsibility |
|---|---|
| `cmd/harness` | flag parsing, wiring, HTTP server lifecycle, graceful shutdown |
| `internal/model` | core types: `AgentBlueprint`, `Pipeline`, `Step`, `Job`, `StepResult` |
| `internal/registry` | load + validate + atomically write registry files |
| `internal/llm` | `Client` interface, OpenAI-compatible impl, fake for tests |
| `internal/engine` | worker pool, job state machine, step execution, restart requeue |
| `internal/pipeline` | DAG resolution, template grammar, router conditions |
| `internal/store` | `Store` interface + SQLite implementation (Redis adapter later) |
| `internal/api` | HTTP handlers + middleware (auth, logging, recovery) |

## Execution model

1. A client `POST`s to a `/execute` endpoint. The handler validates the target exists, writes
   a `Job` (`PENDING`) to the store, enqueues it, and returns `202 {session_id}`.
2. A worker dequeues the job, marks it `RUNNING`, and executes steps:
   - **agent step:** resolve input template → load blueprint + prompt → call `llm.Client` →
     persist named output + a `StepResult` (timing/tokens/status).
   - **router step:** evaluate route conditions against the resolved input → select next step(s).
   - **fan-out/fan-in:** steps with multiple successors run in parallel; a join step waits for
     all declared predecessors.
3. On success the job becomes `COMPLETED`; on error `FAILED` (with a message). Clients poll
   `GET /v1/sessions/{id}`.

## Job state machine

```
PENDING ──► RUNNING ──► COMPLETED
                │
                ├────► FAILED
                └────► CANCELED
```

On startup, jobs left in `RUNNING` (process died mid-flight) are requeued to `PENDING`.

## Non-functional decisions

- **Concurrency:** bounded worker pool (default 1–2) because the target host has 2 cores and
  local inference serializes. See `docs/MEMORY.md`.
- **Persistence:** SQLite in WAL mode; the `Store` interface isolates the engine from storage.
- **Security:** default bind `127.0.0.1`, bearer-token auth when exposed, tools gated by
  `ENABLE_TOOLS` and an allowlist. Registry IDs are sanitized.
- **Observability:** per-step `StepResult` rows; structured logging; `/healthz` + `/readyz`;
  metrics endpoint planned (Phase 9).
