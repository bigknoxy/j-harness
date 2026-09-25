# Evals

`j-harness` ships a small, deterministic evaluation suite so behavior regressions in
the checked-in registry are caught on every PR. It is a plain `go test` and needs no
model server: the cases run against a scripted OpenAI-compatible stub.

## Run it (offline, deterministic default)

```bash
go test ./internal/eval/...
# or, equivalently:
make eval
```

Each case is scored `CORRECT` or `INCORRECT`; the test fails if any case is incorrect.
A summary line (`eval summary: N passed / M failed`) is printed on every run.

```
=== RUN   TestEvalSuite
    eval_test.go:...: CORRECT   triage_billing: ok
    ...
    eval_test.go:...: eval summary: 9 passed / 0 failed (of 9 cases)
--- PASS: TestEvalSuite
```

### Case data

Cases live in [`internal/eval/cases.json`](../internal/eval/cases.json). Each case
names an agent or pipeline from `agent-registry/`, a scripted stub response (ordered
or selected by system prompt), and explicit expectations:

| Field | Meaning |
|---|---|
| `kind` | `agent` or `pipeline` |
| `agent` / `pipeline` | registry target id |
| `input` / `inputs` | agent input string, or pipeline named inputs |
| `script` | ordered stub responses (`content` or `tool_name`+`tool_args`) |
| `script_by_prompt` | stub responses selected by a system-prompt substring (`*` = fallback) |
| `expect.result` | exact final result |
| `expect.json_fields` | JSON fields the result must contain |
| `expect.schema` | registry schema the result must validate against (via `internal/schema`) |
| `expect.calls` | exact number of model calls (proves tool/repair loops) |
| `expect.failed` | the run must fail (fail-closed assertions) |
| `expect.steps_completed` / `expect.steps_skipped` | pipeline routing assertions |

Cases cover: triage classification (billing / technical / other), output-schema
conformance and the single repair turn, fail-closed on unrepairable output,
`support_flow` branch routing, and the bounded tool-call loop (`calculator`).

## Optional: run against a real model

The suite never makes network calls in CI. To replay the same cases against a real
OpenAI-compatible endpoint (e.g. NVIDIA NIM or a local Ollama), copy the scripted
inputs into a real run. The harness binary already supports this:

```bash
export OPENAI_BASE_URL=https://integrate.api.nvidia.com/v1   # or http://127.0.0.1:11434/v1
export OPENAI_MODEL=nvidia/nemotron-3-nano-omni-30b-a3b-reasoning
export OPENAI_API_KEY=...            # never commit this
ENABLE_TOOLS=true ./bin/harness --registry ./agent-registry --db /tmp/eval.db
```

Then submit an agent or pipeline from `cases.json`:

```bash
curl -s localhost:8080/v1/agents/triage/execute \
  -H 'Content-Type: application/json' \
  -d '{"input_data":"I was charged twice on my last invoice."}'
# poll GET /v1/sessions/{session_id} until COMPLETED
```

This is a manual/nightly exercise (see R3 in `docs/research/RECOMMENDATIONS.md`).
It is wired into the scheduled (non-required) [`nightly`](../.github/workflows/nightly.yml)
workflow as `scripts/eval_live.sh`, which replays the billing and technical triage cases
against the real model. The job skips, not fails, when no `NVIDIA_API_KEY` secret is set, so
it is never a cost or flakiness gate on a PR.
