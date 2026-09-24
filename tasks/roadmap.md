# Roadmap

Live phase tracker. Update the Status column as phases complete. v1 ships at end of Phase 5.

| Phase | Deliverable | Status |
|---|---|---|
| 0 | Bootstrap: git, CI, docs, health endpoint, container | ✅ done |
| 1 | `registry`: load/validate/atomic-write blueprints + prompts | ✅ done |
| 2 | `llm.Client` + fake; single-agent execution | ⬜ todo |
| 3 | HTTP API (sync) + middleware (auth/logging/recovery) | ⬜ todo |
| 4 | `store` (SQLite) + async jobs + bounded worker pool + orphan requeue | ⬜ todo |
| 5 | Sequential pipeline (named IO) + template resolver — **v1** | ⬜ todo |
| 6 | Fan-out/fan-in + `router` primitive | ⬜ todo |
| 7 | Registry CRUD API (create/update/list agents & pipelines) | ⬜ todo |
| 8 | Tools / function calling (gated) | ⬜ todo |
| 9 | Hardening: retries/backoff, JSON-schema validation, metrics | ⬜ todo |
| 10 | Docker + compose (harness + ollama), GitOps deploy docs, Pages | ⬜ todo |
| 11 | Optional Redis `Store` adapter | ⬜ todo |

Legend: ⬜ todo · 🚧 in progress · ✅ done · ⛔ blocked
