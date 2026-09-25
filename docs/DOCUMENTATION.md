# Documentation map

Single index of every documentation surface in this repo: what it is the source of truth
for, and which code changes require it to be updated. Use this when reviewing a diff for
docs drift.

## Hard rule: every PR updates docs or states no impact

Documentation is not optional after a change. Every pull request must either:

1. update the doc surfaces this change affects (the map below says which), or
2. check "no docs impact" in the PR template and say why in one line.

This is enforced by CI. The always-run `docs` job (see `.github/workflows/ci.yml`)
executes the pure-Go drift test in `internal/docscheck`, which fails when the code and the
docs disagree about routes, environment variables, metrics counters, the version pin, or
relative links. Run it locally with `make docs`.


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
| `docs/EVALS.md` | eval suite: how to run it offline, the case schema, and the optional real-model replay | `internal/eval/cases.json` fields change, a case category is added, the run commands change |
| `docs/MEMORY.md` | append-only decision log (newest first); why things are the way they are | any architectural or product decision is made or reversed (add a new entry; never rewrite history) |
| `docs/DOCUMENTATION.md` | this map | a doc surface is added, removed, or changes ownership |
| `tasks/roadmap.md` | phase/delivery tracker and statuses | a phase starts or completes |
| `tasks/todo.md` | current work item, next action, blockers, done log (with verification) | before starting and after finishing any work item |
| `tasks/lessons.md` | failure modes with detection signal + prevention rule | after any correction or postmortem |
| `docker-compose.yml` | local harness + ollama topology, env wiring, healthchecks | services, ports, volumes, or env wiring change |
| `Dockerfile` | image build stages, runtime user, stamped version | base images, build args, or runtime layout change |
| `scripts/smoke.sh` | post-build smoke assertions (`/healthz`, `/readyz`, `/metrics`, 404 path) | a smoke-critical endpoint is added or changes |
| `internal/e2e` | real HTTP end-to-end coverage over the full stack | an endpoint, job lifecycle, routing, auth, or registry-write behavior changes |
| `internal/e2e/testdata` | golden files pinning the exact session/step/error wire format | the JSON shape of any API response changes (regenerate with `UPDATE_GOLDEN=1`) |
| `internal/docscheck` | the offline docs drift gate: routes, env vars, metrics, version pin, links, ASCII | add or change a check, or an allowlisted exception |
| `internal/eval/cases.json` | the checked-in eval cases and their expectations | a new regression case is needed, or an expectation changes |
| `docs/EVALS.md` | how to run evals | the runner or case schema changes |
| `install.sh` / `uninstall.sh` | install/uninstall behavior, overridable vars, help text | install layout, download URLs, or overridable vars change |
| `.github/workflows/*.yml` | CI (lint/test/coverage/vuln/dependency-review/docs/pr-title/smoke), release (GoReleaser), Pages publish, CodeQL | jobs, triggers, Go version, tool versions, or publish steps change |
| `.github/dependabot.yml` | automated dependency + action-SHA updates | ecosystems, schedule, or grouping change |
| `.github/CODEOWNERS` | code ownership / automatic review requests | ownership or path rules change |
| `scripts/coverage.sh` | the coverage floor (statement coverage vs `COVERAGE_THRESHOLD`) | coverage rule, profile flags, or threshold change |
| `scripts/container_e2e.sh` / `scripts/stub_llm.py` | scheduled container E2E (build image, run it, drive the async API over real HTTP) | the container topology, stub behavior, or the API exercised changes |
| `scripts/eval_live.sh` | scheduled real-model eval replay | the cases replayed or their run wiring change |
| `.github/workflows/nightly.yml` | the scheduled (non-required) container + real-model checks | the schedule, secrets/vars, or the scripts they call change |
| `.github/PULL_REQUEST_TEMPLATE.md` | the docs-checklist contract for every PR | the docs rule or verification steps change |
| `.goreleaser.yaml` | release archive naming and build matrix | release artifacts or naming change |

## Consistency checklist

When syncing docs, cross-check these shared facts against the code:

- **Status:** phases 0-11 complete; release `v0.2.0`.
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
- **Makefile gate:** `make fmt-check vet test build`; `make e2e`, `make eval`, `make docs`,
  and `make coverage` run the HTTP end-to-end, eval, docs-drift, and coverage checks alone.
- **Scheduled (not required):** `.github/workflows/nightly.yml` runs `scripts/container_e2e.sh`
  (Docker image + real HTTP) and `scripts/eval_live.sh` (real model, skipped without a key).
- **Style:** ASCII only in docs, no em-dashes, no emojis, no AI tells.

## Automated drift check

`internal/docscheck` (run by `make docs` and the CI `docs` job) parses the code and the
docs and fails on any mismatch:

- **Routes:** every route registered in `internal/api` must appear in `README.md` and
  `docs/API.md`, and neither may list a route that no longer exists.
- **Env vars:** every variable read in Go source must appear in the `docs/API.md` or
  `docs/DEPLOY.md` environment tables, and neither may document a variable nothing reads.
- **Metrics:** every counter in `internal/metrics` must appear in the `docs/API.md`
  counters table, and vice versa.
- **Version:** the `VERSION=` pin in `README.md` must match the one in `docs/DEPLOY.md`.
- **Links:** every relative markdown link must resolve to a real file (and `#anchor` to a
  real heading).
- **ASCII:** no non-ASCII bytes in any markdown surface or `index.html`.

The test needs no network and no dependency, and it fails (never skips) on drift.

## Manual drift sweep

A quick sweep for stale language before merging a docs change:

```bash
grep -rn -E 'Phase 10|Phase 5|Planned|later phase|arrive in|next.*DAG|optional Redis store adapter \| todo|TODO|TBD' \
  README.md index.html AGENTS.md docs tasks .github docker-compose.yml Dockerfile scripts install.sh uninstall.sh
```

Resolve every real hit; historical references in `docs/MEMORY.md` (a decision made at
Phase N) are legitimate and should stay.
