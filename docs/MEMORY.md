# Memory — decision log

Architectural and product decisions, newest first. Format: `YYYY-MM-DD HH:MM: <summary>`.

Use this file when you need to know **why** something is the way it is. If a decision here is
reversed, add a new entry (do not delete the old one).

---

- **2026-09-24 04:00: v1 scope = core + async queue.** v1 (end of Phase 5) delivers the
  file registry, single-agent + sequential-pipeline execution, and an async HTTP API with
  status polling backed by a bounded in-process worker pool. No Redis, no CRUD API, no tools.
  Rationale: matches "super basic" while remaining useful on a 2-core homelab host.

- **2026-09-24 04:00: Embedded SQLite behind a `Store` interface (not Redis).** Job state and
  status live in an embedded SQLite DB (WAL mode); the registry stays on disk as git-tracked
  files. The `store` package exposes an interface so a Redis adapter (Phase 11) can be added
  without touching the engine. Rationale: one binary, no external services, restart recovery,
  and the "enterprise" scale-out path stays open.

- **2026-09-24 04:00: Pipeline model = DAG + named IO + conditional router.** Steps declare
  explicit named inputs/outputs resolved via a strict, non-evaluating template grammar
  (`{{ inputs.x }}`, `{{ steps.<id>.output }}`). A dedicated `router` step selects the next
  step from labeled routes based on a condition DSL (`equals|not_equals|in|matches|exists`).
  Rationale: supports sequential chains, fan-out/fan-in, and triage→route→respond without a
  special-case engine; a linear-only schema would force a rework at the first branch.

- **2026-09-24 04:00: Async by default.** All executions return `202` + `session_id`; results
  are polled via `GET /v1/sessions/{id}`. Rationale: local CPU inference can take minutes and
  would otherwise time out synchronous HTTP requests.

- **2026-09-24 04:00: Bounded worker pool, not goroutine-per-request.** Concurrency is capped
  (default 1–2). Rationale: a 2-core host cannot parallelize local inference; unbounded
  goroutines would thrash CPU and memory.

- **2026-09-24 04:00: Tools are off by default and treated as RCE.** `ENABLE_TOOLS=false`,
  per-deployment allowlist, auth required, registry IDs sanitized to `[a-z0-9_-]+`, and
  untrusted pipeline inputs must not reach tool-enabled agents. Rationale: a `run_bash` tool
  behind an unauthenticated API is remote code execution.

- **2026-09-24 04:00: No new dependencies without a logged decision.** Default to the standard
  library and the existing stack. Rationale: keep the binary small and the supply chain minimal.

- **2026-09-24 04:00: Public repo, Pages publishes the README.** `gh-pages.yml` uses
  `peaceiris/actions-gh-pages@v3` to publish from repo root; `.nojekyll` committed. Rationale:
  README is the landing page / source of truth (project rule).
