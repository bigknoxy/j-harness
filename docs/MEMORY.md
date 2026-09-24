# Memory — decision log

Architectural and product decisions, newest first. Format: `YYYY-MM-DD HH:MM: <summary>`.

Use this file when you need to know **why** something is the way it is. If a decision here is
reversed, add a new entry (do not delete the old one).

---

- **2026-09-24: Phase 7 — registry CRUD writes reload the whole registry and hot-swap the engine snapshot.**
  `POST|PUT /v1/registry/{agents,pipelines}/{id}` validate the payload, call the existing
  `registry.WriteBlueprint`/`WritePipeline` (atomic temp-file + rename), then re-`Load` the
  entire registry root and swap the resulting snapshot into both the API and the `Engine`.
  We reload rather than mutate maps in place because the snapshot then matches exactly what a
  fresh process would load, and one validation pass catches cross-file breakage (a new
  pipeline referencing an agent that does not exist, a duplicate output name, a cycle). The
  `Engine` now holds its registry behind a `sync.RWMutex` with `SetRegistry`; an in-flight run
  keeps the snapshot it started with, and the next run sees the new one. Writes are serialized
  by the API's `mu` so concurrent reloads cannot interleave. `POST` returns `409` on an
  existing id, `PUT` returns `404` on a missing id; an invalid payload returns `400` and
  leaves the files untouched.

- **2026-09-24: Phase 6 — dependencies are derived from refs, `needs`, and router edges.**
  `Engine.RunPipeline` is a concurrent DAG scheduler. Step dependencies come from three
  sources, unioned: (a) `{{ steps.<id>.output }}` refs in a step's input, (b) an explicit
  `needs: [ids]` list on the step, and (c) a router's route `goto` targets. Deriving from refs
  keeps linear chains terse (no boilerplate edges); `needs` makes joins explicit and readable;
  router gotos are control-flow edges regardless. Independent steps run concurrently, bounded
  by `GOMAXPROCS` (the host has 2 cores and local inference serializes, so unbounded fan-out
  would only thrash). A step whose predecessor `FAILED` or was `SKIPPED` is itself `SKIPPED`,
  so branch losers are recorded without running. Cycles and unknown refs/needs are rejected at
  registry **load** time (`internal/pipeline/graph.go: Deps`), keeping bad pipelines out of
  the engine.
- **2026-09-24: Dogfooded against a real provider (NVIDIA NIM).**
  Ran the shipped registry through the async API end to end. `triage` classified a billing
  complaint as `{"category":"billing","priority":"high"}` (203 tokens) and a crash report as
  `technical`; `support_flow` routed to `billing_reply` / `tech_reply` respectively and marked
  the other two branches `SKIPPED`, returning the agent reply as the run result. Provider was
  NVIDIA NIM (`nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`) via `OPENAI_BASE_URL`; the
  homelab Ollama box was saturated (a 0.8B completion took >150s), so NIM was used instead.
  Confirms the OpenAI-compatible client works against a real hosted endpoint.
- **2026-09-24: JSON Schemas live inside the registry bundle (`agent-registry/schemas/`).**
  Blueprints reference schemas with a path relative to the registry root
  (`schemas/triage.json`), so the schema directory must sit **inside** `agent-registry/` for
  the reference to resolve. It previously lived at the repo top level, which made a fetched
  registry non-self-contained and caused `uninstall.sh` to leave an orphaned directory. The
  registry is now one self-contained bundle that install/run/Docker/uninstall treat as a unit.
- **2026-09-24: Delivery track — releases, installer, Pages, branch protection.**
  Versioned releases are cut by **GoReleaser** on `v*` tags (`.goreleaser.yaml` +
  `.github/workflows/release.yml`), publishing linux/darwin/windows amd64+arm64 archives,
  `checksums.txt`, and auto-generated notes to GitHub Releases. Archive names are
  **version-less** (`j-harness_<os>_<arch>`) so `releases/latest/download/<asset>` is a
  stable URL and the installer needs no API call. `install.sh` / `uninstall.sh` are plain
  POSIX `sh`, verify the sha256 from `checksums.txt`, and install to `~/.local/bin` +
  `~/.config/j-harness/registry` (all overridable). The Pages site is now a hand-written
  `index.html` published verbatim by `gh-pages.yml` with `enable_jekyll: false`; `_config.yml`
  was deleted (Jekyll no longer processes the site). `main` is PR-only with required checks
  `lint`, `test (ubuntu-latest, 1.27)`, `test (macos-latest, 1.27)`, `vuln`, and `smoke`,
  linear history, and **admin bypass allowed** (`enforce_admins: false`)
  since the repo is single-maintainer. Rationale: stable download URLs, no Jekyll surprises,
  and a repo that reads professionally without AI tells.
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
