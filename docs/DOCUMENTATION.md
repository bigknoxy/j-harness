# Documentation map

Single index of every documentation surface in this repo: what it is the source of truth
for, and which code changes require it to be updated. Use this when reviewing a diff for
docs drift.

## Surfaces

| Surface | Source of truth for | Update when |
|---|---|---|
| `README.md` | the project pitch: what it is, status, install, quickstart, feature list, HTTP API summary, roadmap table, doc links, badges | a public feature ships, an endpoint/env var changes, a phase completes, install/quickstart behavior changes |
| `index.html` | the GitHub Pages landing page (served at https://bigknoxy.github.io/j-harness/); quickstart, feature cards, API table, roadmap summary | same triggers as `README.md`; keep it in step with the README |
| `AGENTS.md` | cold-start resume instructions: what the repo is, where to look, commands, conventions, definition of done | command/Makefile targets change, conventions change, a new resume pointer is needed |
| `docs/ARCHITECTURE.md` | package layout, execution model, hardening, job state machine, non-functional decisions | a package is added/renamed, the execution model or job lifecycle changes, a hardening feature ships |
| `docs/API.md` | every HTTP endpoint, status codes, auth, error envelope, registry management, metrics counters, environment variables | a route is added/changed/removed, a status code changes, an env var is added/renamed/removed, metrics change |
| `docs/SCHEMA.md` | blueprint/pipeline/job field reference, registry layout, DAG execution, template grammar, router conditions | a blueprint/pipeline/job field is added or changes meaning, the registry layout changes, the template grammar or router DSL changes |
| `docs/DEPLOY.md` | image build, compose, full env table, GitOps deploy model, job store (SQLite/Redis), operating notes, hardening checklist | Dockerfile/compose changes, an env var is added/removed, the store options change, deploy/operating guidance changes |
| `docs/MEMORY.md` | append-only decision log (newest first); why things are the way they are | any architectural or product decision is made or reversed (add a new entry; never rewrite history) |
| `docs/DOCUMENTATION.md` | this map | a doc surface is added, removed, or changes ownership |
| `tasks/roadmap.md` | phase/delivery tracker and statuses | a phase starts or completes |
| `tasks/todo.md` | current work item, next action, blockers, done log (with verification) | before starting and after finishing any work item |
| `tasks/lessons.md` | failure modes with detection signal + prevention rule | after any correction or postmortem |
| `docker-compose.yml` | local harness + ollama topology, env wiring, healthchecks | services, ports, volumes, or env wiring change |
| `Dockerfile` | image build stages, runtime user, stamped version | base images, build args, or runtime layout change |
| `scripts/smoke.sh` | post-build smoke assertions (`/healthz`, `/readyz`, `/metrics`, 404 path) | a smoke-critical endpoint is added or changes |
| `install.sh` / `uninstall.sh` | install/uninstall behavior, overridable vars, help text | install layout, download URLs, or overridable vars change |
| `.github/workflows/*.yml` | CI (lint/test/vuln/smoke), release (GoReleaser), Pages publish | jobs, triggers, Go version, or publish steps change |
| `.goreleaser.yaml` | release archive naming and build matrix | release artifacts or naming change |

## Consistency checklist

When syncing docs, cross-check these shared facts against the code:

- **Status:** phases 0-11 complete; release `v0.1.0`.
- **Endpoints:** `GET /healthz`, `GET /readyz`, `GET /metrics` (unauthenticated);
  `POST /v1/agents/{id}/execute`, `POST /v1/pipelines/{id}/execute` -> `202 {session_id}`;
  `GET /v1/sessions/{id}`, `GET /v1/sessions/{id}/steps`;
  `GET|POST|PUT /v1/registry/{agents,pipelines}[/{id}]` (create `409`, update `404`).
- **Auth:** `HARNESS_AUTH_TOKEN` guards every `/v1/*` route with a bearer token.
- **Errors:** envelope `{"error":{"code","message"}}`.
- **Store backends:** `sqlite` (default) or `redis` via `HARNESS_STORE`.
- **Env vars/defaults:** see the tables in `docs/API.md` and `docs/DEPLOY.md`.
- **Registry contents:** blueprints `triage`, `generic_agent`, `summarizer`,
  `risk_assessor`, `calculator`; pipelines `support_flow`, `digest_flow`; schema
  `schemas/triage.json`.
- **Makefile gate:** `make fmt-check vet test build`.
- **Style:** ASCII only in docs, no em-dashes, no emojis, no AI tells.

## Drift check

A quick sweep for stale language before merging a docs change:

```bash
grep -rn -E 'Phase 10|Phase 5|Planned|later phase|arrive in|next.*DAG|optional Redis store adapter \| todo|TODO|TBD' \
  README.md index.html AGENTS.md docs tasks .github docker-compose.yml Dockerfile scripts install.sh uninstall.sh
```

Resolve every real hit; historical references in `docs/MEMORY.md` (a decision made at
Phase N) are legitimate and should stay.
