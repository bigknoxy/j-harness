# Schema reference

Agent behavior is data: Markdown prompts + JSON blueprints/pipelines. All JSON files carry a
`version` field and are validated on load (and on every write via the registry CRUD API).

## Blueprint - `agent-registry/blueprints/<id>.json`

Defines one agent: which prompt to load, which model/params, and (optionally) tools.

```json
{
  "id": "triage",
  "description": "Classify an incoming support email.",
  "prompt_path": "prompts/triage.md",
  "model": "llama3.2",
  "base_url": "http://ollama:11434/v1",
  "temperature": 0.0,
  "max_tokens": 1024,
  "timeout_seconds": 300,
  "output_format": "json",
  "output_schema": "schemas/triage.json",
  "tools": [],
  "version": 1
}
```

| Field | Type | Notes |
|---|---|---|
| `id` | string | must match filename; `[a-z0-9_-]+` |
| `prompt_path` | string | relative to registry root |
| `model` | string | passed through to the endpoint |
| `base_url` | string | optional; overrides the global endpoint |
| `temperature` | float | optional |
| `max_tokens` | int | optional |
| `timeout_seconds` | int | per-call timeout (default from config) |
| `output_format` | string | `text` (default) or `json` |
| `output_schema` | string | optional JSON Schema (draft-07 subset) for `json` output; requires `output_format: "json"`. Compiled at load and enforced at runtime with one repair turn on mismatch |
| `tools` | []string | built-in tool names; only honored when `ENABLE_TOOLS=true`. Built-ins: `current_time`, `word_count`, `math_eval`. Unknown names are rejected on load |
| `version` | int | schema version (currently `1`) |

## Pipeline - `agent-registry/pipelines/<id>.json`

A DAG of steps. Steps have a **named output** that later steps reference.

```json
{
  "pipeline_id": "support_flow",
  "version": 1,
  "inputs": ["user_input"],
  "steps": [
    { "id": "triage", "agent_id": "triage",
      "input": "{{ inputs.user_input }}", "output": "classification" },

    { "id": "route", "router": true, "input": "{{ steps.triage.output }}",
      "routes": [
        { "when": { "field": "category", "equals": "billing" }, "goto": "billing_reply" },
        { "when": { "field": "category", "equals": "technical" }, "goto": "tech_reply" },
        { "default": true, "goto": "generic_reply" }
      ] },

    { "id": "billing_reply", "agent_id": "billing_agent",
      "input": "{{ inputs.user_input }}", "output": "reply" },
    { "id": "tech_reply", "agent_id": "tech_agent",
      "input": "{{ inputs.user_input }}", "output": "reply" },
    { "id": "generic_reply", "agent_id": "generic_agent",
      "input": "{{ inputs.user_input }}", "output": "reply" }
  ],
  "output": "{{ steps.generic_reply.output }}"
}
```

> Note: `steps.<id>` is the **step id**, not the output name. Two steps may share an
> output name (e.g. several branch replies), so a pipeline output that must capture "whichever
> branch ran" should **omit the top-level `output`**. With no `output` template, the result is
> the last agent step that executed. Steps not reached by a router are recorded as `SKIPPED`.

### DAG execution

Steps form a directed acyclic graph. A step's dependencies come from three sources:

- **template references** in its `input` (`{{ steps.<id>.output }}`)
- an explicit **`needs`** list of step ids
- **router branch edges**: every `goto` target depends on its router step

A step runs once all its dependencies have completed. Independent steps (for example the two
fan-out legs of a diamond) run **concurrently**, bounded by the engine's parallelism limit
(`GOMAXPROCS`). A join step lists its predecessors via `needs` and/or by referencing their
outputs, then merges them. Cycles are rejected at load time.

```json
{
  "id": "digest_flow",
  "steps": [
    { "id": "summarize", "agent_id": "summarizer",
      "input": "{{ inputs.user_input }}", "output": "summary" },
    { "id": "assess_risk", "agent_id": "risk_assessor",
      "input": "{{ inputs.user_input }}", "output": "risk" },
    { "id": "combine", "agent_id": "generic_agent",
      "input": "{{ steps.summarize.output }} | {{ steps.assess_risk.output }}",
      "output": "digest", "needs": ["summarize", "assess_risk"] }
  ],
  "output": "{{ steps.combine.output }}"
}
```

`summarize` and `assess_risk` run at the same time; `combine` waits for both.

### Template grammar

Strict, non-evaluating, validated at load time:

- `{{ inputs.<name> }}` - a pipeline input
- `{{ steps.<id>.output }}` - a step's named output
- `{{ steps.<id>.output.<json.path> }}` - a JSON path into a step output

Anything else is a load error. No shell, no expressions, no function calls.

### Router conditions

A `router` step picks successors; each route has `when` or `default`:

| Operator | Meaning |
|---|---|
| `equals` / `not_equals` | scalar equality against `field` |
| `in` | `field` value is a member of the given list |
| `matches` | `field` value matches a regex |
| `exists` | `field` is present and non-empty |

`field` is a JSON path into the router step's resolved input.

## Job / session (runtime, stored in SQLite or Redis)

| Field | Meaning |
|---|---|
| `session_id` | UUID returned by `/execute` |
| `kind` | `agent` or `pipeline` |
| `target_id` | agent or pipeline id |
| `status` | `PENDING` - `RUNNING` - `COMPLETED` - `FAILED` - `CANCELED` |
| `input` | initial input payload |
| `result` | final output (on success) |
| `error` | failure message (on failure) |
| `step_results` | per-step timing / tokens / status |

## Registry layout

```
agent-registry/
|-- blueprints/<id>.json
|-- prompts/<id>.md
|-- pipelines/<id>.json
`-- schemas/<name>.json      # optional JSON Schemas referenced by blueprints
```
