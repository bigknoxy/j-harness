# API reference

Base URL: `http://127.0.0.1:8080` (default). All bodies are JSON.

## Authentication

When `HARNESS_AUTH_TOKEN` is set, every `/v1/*` request must send:

```
Authorization: Bearer <token>
```

`/healthz` and `/readyz` are always unauthenticated.

## Execute

> **Phase 3 note:** execution is currently **synchronous** — the request blocks until the
> model responds and returns the result directly. The async submit/poll shape (`202` +
> `GET /v1/sessions/{id}`) lands in Phase 4. The request/response fields below are forward
> compatible: the same `input_data` payload is used, and the eventual async `result` holds
> the same `output` string.

### `POST /v1/agents/{id}/execute`

```bash
curl -X POST http://127.0.0.1:8080/v1/agents/generic_agent/execute \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $HARNESS_AUTH_TOKEN" \
  -d '{"input_data": "Summarize this in one line."}'
```

**200 OK**

```json
{
  "agent_id": "generic_agent",
  "output": "...",
  "tokens": 42,
  "duration_ms": 812
}
```

Errors use a JSON envelope: `{"error":{"code":"...","message":"..."}}`.

## Health

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | process is alive |
| `GET /readyz` | dependencies (store) are ready |

## Status codes

| Code | Meaning |
|---|---|
| `200` | OK |
| `400` | malformed request / invalid payload |
| `401` | missing or invalid bearer token |
| `404` | unknown agent, pipeline, session, or route |
| `405` | wrong method |
| `500` | internal error |
| `503` | not ready (planned for Phase 4 async readiness) |

## Planned (later phases)

- `POST /v1/pipelines/{id}/execute` — run a pipeline
- async submit (`202 Accepted` + `session_id`) and `GET /v1/sessions/{id}` polling
- `GET /v1/sessions/{id}/steps` — per-step results
- `POST /v1/sessions/{id}/cancel`
- `GET|POST|PUT|DELETE /v1/registry/{agents,pipelines}` — manage agents/pipelines over HTTP
- `GET /metrics` — Prometheus metrics
