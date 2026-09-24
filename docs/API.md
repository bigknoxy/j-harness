# API reference

Base URL: `http://127.0.0.1:8080` (default). All bodies are JSON.

## Authentication

When `HARNESS_AUTH_TOKEN` is set, every `/v1/*` request must send:

```
Authorization: Bearer <token>
```

`/healthz` and `/readyz` are always unauthenticated.

## Async model

Execution is asynchronous. Submit a job and poll for its result.

1. `POST` to an `/execute` endpoint returns **`202 Accepted`** with a `session_id`.
2. `GET /v1/sessions/{id}` returns the job's `status` and, once terminal, its `result`.
3. `GET /v1/sessions/{id}/steps` returns per-step results (pipeline runs).

Job status is one of `PENDING`, `RUNNING`, `COMPLETED`, `FAILED`, `CANCELED`.

## Submit an agent

```bash
curl -s -X POST http://127.0.0.1:8080/v1/agents/generic_agent/execute \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $HARNESS_AUTH_TOKEN" \
  -d '{"input_data": "Summarize this in one line."}'
```

**202 Accepted**

```json
{ "session_id": "sess_1f9c...", "status": "PENDING" }
```

## Submit a pipeline

The body is a JSON object mapping each declared pipeline input to a string.

```bash
curl -s -X POST http://127.0.0.1:8080/v1/pipelines/support_flow/execute \
  -H "Content-Type: application/json" \
  -d '{"user_input": "I was charged twice this month."}'
```

**202 Accepted**

```json
{ "session_id": "sess_44a0...", "status": "PENDING" }
```

## Poll a session

### `GET /v1/sessions/{id}`

**200 OK**

```json
{
  "session_id": "sess_44a0...",
  "kind": "pipeline",
  "target_id": "support_flow",
  "status": "COMPLETED",
  "result": "..."
}
```

On failure, `status` is `FAILED` and `error` carries the message.

### `GET /v1/sessions/{id}/steps`

**200 OK**

```json
{
  "session_id": "sess_44a0...",
  "steps": [
    { "step_id": "triage", "status": "COMPLETED", "output": "{...}", "tokens": 42, "duration_ms": 512 },
    { "step_id": "route", "status": "COMPLETED" },
    { "step_id": "billing_reply", "status": "COMPLETED", "output": "...", "tokens": 30, "duration_ms": 400 },
    { "step_id": "tech_reply", "status": "SKIPPED" },
    { "step_id": "generic_reply", "status": "SKIPPED" }
  ]
}
```

## Registry management

Agents and pipelines are managed as files on disk, but the same registry can be
edited over HTTP. Writes validate before touching disk (a bad payload leaves the
registry unchanged), persist atomically, then reload the registry and hot-swap
the engine snapshot. In-flight jobs finish against the snapshot they started
with; new jobs see the change immediately.

All registry routes are under `/v1/registry/` and require the bearer token when
`HARNESS_AUTH_TOKEN` is set (like every other `/v1/*` route).

| Method + path | Purpose |
|---|---|
| `GET /v1/registry/agents` | list agents (id, description, model, output_format) |
| `GET /v1/registry/agents/{id}` | read one agent (blueprint + prompt) |
| `POST /v1/registry/agents/{id}` | create an agent (`409` if it already exists) |
| `PUT /v1/registry/agents/{id}` | update an agent (`404` if it does not exist) |
| `GET /v1/registry/pipelines` | list pipelines |
| `GET /v1/registry/pipelines/{id}` | read one pipeline |
| `POST /v1/registry/pipelines/{id}` | create a pipeline (`409` if it already exists) |
| `PUT /v1/registry/pipelines/{id}` | update a pipeline (`404` if it does not exist) |

Create/update an agent with `{"blueprint": {...}, "prompt": "..."}`. The
blueprint `id` must match the path id. Create/update a pipeline with
`{"pipeline": {...}}`; `pipeline_id` must match the path id.

**200 OK**

```json
{ "status": "ok" }
```

Example: create an agent.

```bash
curl -s -X POST http://127.0.0.1:8080/v1/registry/agents/scribe \
  -H 'Content-Type: application/json' \
  -d '{
    "blueprint": {
      "id": "scribe",
      "prompt_path": "prompts/scribe.md",
      "model": "qwen3:8b",
      "output_format": "text",
      "version": 1
    },
    "prompt": "You summarize text into three bullet points."
  }'
```

## Health

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | process is alive |
| `GET /readyz` | dependencies (registry, store) are ready |

## Status codes

| Code | Meaning |
|---|---|
| `200` | OK |
| `202` | job accepted (async submit) |
| `400` | malformed request / invalid payload |
| `401` | missing or invalid bearer token |
| `404` | unknown agent, pipeline, session, or route |
| `405` | wrong method |
| `409` | create conflict (agent/pipeline already exists) |
| `500` | internal error |
| `503` | queue full or dependency not ready |

Errors use a JSON envelope: `{"error":{"code":"...","message":"..."}}`.

## Planned (later phases)

- `POST /v1/sessions/{id}/cancel`
- `GET /metrics` — Prometheus metrics
