# Memory — decision log

Architectural and product decisions, newest first. Format: `YYYY-MM-DD HH:MM: <summary>`.

Use this file when you need to know **why** something is the way it is. If a decision here is
reversed, add a new entry (do not delete the old one).

---

- **2026-09-24: Phase 5 (v1) — pipelines run sequentially; omitting `output` means "last step".**
  `Engine.RunPipeline` walks steps in declared order. Router steps select one successor and
  mark the other branch targets `SKIPPED` (recorded as step rows). Because a skipped branch's
  output never exists, `support_flow` **omits the top-level `output`**: with no `output`
  template the result is the last agent step that executed, which is exactly "whichever branch
  ran". Per-step outcomes are persisted as `StepResult` rows by the worker, so
  `GET /v1/sessions/{id}/steps` shows the full path including skips. Template runtime
  resolution lives in `internal/pipeline/resolve.go`; the grammar/validation from Phase 1 is
  reused so load-time and run-time agree.

- **2026-09-24: Pure-Go SQLite driver + Go 1.27 (current stable).** Phase 4 adds
  `modernc.org/sqlite` (v1.59.0), a CGO-free SQLite so the existing `CGO_ENABLED=0`
  static build and Alpine image keep working. This is the first third-party runtime
  dependency; approved because the standard library has no SQL database and a pure-Go
  driver avoids CGO/cross-compile pain. Toolchain, Dockerfile, and CI all track the
  current stable Go (1.27.1) per user preference ("use current LTS").

- **2026-09-24 04:10: Phase 3 ships the execute endpoint *synchronously*; async is Phase 4.**
  `POST /v1/agents/{id}/execute` blocks on `engine.RunAgent` and returns `200` with
  `{agent_id,output,tokens,duration_ms}` rather than the planned `202 {session_id}`. The
  handler decodes a strict `{"input_data":"..."}` body and the error envelope is
  `{"error":{"code","message"}}`. This keeps the transport thin and proves the engine path
  end to end before the job queue exists; the async shape reuses the same `input_data` field
  and the same `output` string, so it is forward compatible.

- **2026-09-24 04:10: API middleware order = recoverer → logging → auth → mux.** Panic
  recovery is outermost so a panic still produces a logged `500` envelope; auth only guards
  `/v1/*` and is skipped entirely when `HARNESS_AUTH_TOKEN` is unset, keeping `/healthz` and
  `/readyz` always reachable for probes. Request bodies are capped at 1 MiB and decoded with
  `DisallowUnknownFields` to protect the 2-core host.

- **2026-09-24 04:06: Template refs are validated against step *ids*, not output names.**
  `{{ steps.<id>.output }}`/`.path` resolves by step id (output names are free-form and may
  repeat across router branches). Router `goto` may target *later* steps (that is the branch);
  only input/output template refs are checked for forward references, using a two-pass
  validation (collect all ids, then walk in order).

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

- **2026-09-24 04:02: Pages renders README via Jekyll, not raw publish.** `gh-pages.yml` uses
  `peaceiris/actions-gh-pages@v3` to publish from repo root with a `_config.yml`
  (`jekyll-theme-cayman`) and **no `.nojekyll`**. Rationale: a `.nojekyll` marker disables
  Jekyll, so `README.md` is never rendered to `index.html` and the site 404s (observed). README
  is the landing page / source of truth (project rule).
