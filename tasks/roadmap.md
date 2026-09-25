# Roadmap

Live phase tracker. Engine phases 0-11 are all complete. v1 shipped at end of Phase 5.

## Engine phases

| Phase | Deliverable | Status |
|---|---|---|
| 0 | Bootstrap: git, CI, docs, health endpoint, container | done |
| 1 | `registry`: load/validate/atomic-write blueprints + prompts | done |
| 2 | `llm.Client` + fake; single-agent execution | done |
| 3 | HTTP API (sync) + middleware (auth/logging/recovery) | done |
| 4 | `store` (SQLite) + async jobs + bounded worker pool + orphan requeue | done |
| 5 | Sequential pipeline + `router` primitive (named IO), **v1** | done |
| 6 | Parallel DAG fan-out/fan-in (join step) | done |
| 7 | Registry CRUD API (create/update/list agents & pipelines) | done |
| 8 | Tools / function calling (gated) | done |
| 9 | Hardening: retries/backoff, JSON-schema validation, metrics | done |
| 10 | Docker + compose (harness + ollama), GitOps deploy docs | done |
| 11 | Optional Redis `Store` adapter | done |

## Delivery track (professional polish)

| Item | Deliverable | Status |
|---|---|---|
| D1 | Branch protection on `main` (PR-only, admin bypass allowed) | done |
| D2 | CI best practices: lint, test matrix, vuln scan, release job | done |
| D3 | Automated versioned releases (GoReleaser) + GitHub Releases | done |
| D4 | One-line `curl` installer + uninstaller | done |
| D5 | Custom GH Pages site (`index.html`), fun + tech-forward, no AI tells | done |
| D6 | Repo homepage URL -> Pages; topics + description | done |
| D7 | README badges (build, release, Go version, license) | done |
| D8 | Dogfood: run real agents/pipelines via a hosted provider | done |

Legend: `todo` | `in progress` | `done` | `blocked`.
