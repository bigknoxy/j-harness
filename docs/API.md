# API reference

Base URL: `http://127.0.0.1:8080` (default). All bodies are JSON.

## Authentication

When `HARNESS_AUTH_TOKEN` is set, every `/v1/*` request must send:

```
Authorization: Bearer <token>
```

`/healthz` and `/readyz` are always unauthenticated.

## Execute (async)

Execution is asynchronous: the request returns immediately with a session id, and the result
is polled separately. This avoids HTTP timeouts when local CPU models take minutes.

### `POST /v1/agents/{id}/execute`

```bash
curl -X POST http://127.0.0.1:8080/v1/agents/triage/execute \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $HARNESS_AUTH_TOKEN" \
  -d '{"input_data": "My bill has a duplicate charge."}'
```

**202 Accepted**

```json
{ "session_id": "59cfc890-a50d-472d-8647-81498679d9e6", "status": "PENDING" }
```

### `POST /v1/pipelines/{id}/execute`

```bash
curl -X POST http://127.0.0.1:8080/v1/pipelines/support_flow/execute \
  -H "Content-Type: application/json" \
  -d '{"input_data": "Server node down: Error 500 on database write."}'
```

Returns the same `202` shape. For pipelines the `input_data` is bound to the pipeline input
named `user_input` (see `docs/SCHEMA.md`).

## Poll status

### `GET /v1/sessions/{id}`

```json
{
  "session_id": "59cfc890-a50d-472d-8647-81498679d9e6",
  "status": "RUNNING",
  "result": "",
  "error": ""
}
```

Once `status` is `COMPLETED`, `result` holds the final output. On `FAILED`, `error` explains why.

## Health

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | process is alive |
| `GET /readyz` | dependencies (store) are ready |

## Status codes

| Code | Meaning |
|---|---|
| `200` | OK (status reads) |
| `202` | accepted (execution enqueued) |
| `400` | malformed request / invalid payload |
| `401` | missing or invalid bearer token |
| `404` | unknown agent, pipeline, or session |
| `405` | wrong method |
| `500` | internal error |
| `503` | not ready |

## Planned (later phases)

- `GET /v1/sessions/{id}/steps` — per-step results
- `POST /v1/sessions/{id}/cancel`
- `GET|POST|PUT|DELETE /v1/registry/{agents,pipelines}` — manage agents/pipelines over HTTP
- `GET /metrics` — Prometheus metrics
