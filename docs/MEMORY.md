# Memory: decision log

Architectural and product decisions, newest first. Format: `YYYY-MM-DD HH:MM: <summary>`.

Use this file when you need to know **why** something is the way it is. If a decision here is
reversed, add a new entry (do not delete the old one).

---

- **2026-09-25: CI supply-chain hardening: actions pinned by commit SHA, tools pinned by
  version, and security jobs added (Dependabot, dependency review, CodeQL).**
  Every `uses:` is pinned to a 40-char commit SHA with the human tag in a trailing comment,
  because a mutable tag can be repointed at malicious code (the tj-actions/changed-files
  incident). `staticcheck` and `govulncheck` are pinned by version env var instead of
  `@latest`: an unpinned tool release must not be able to break the gate, which golangci-lint
  already did once (Go version ceiling). Checkout uses `persist-credentials: false` so the
  GITHUB_TOKEN is not left in `.git/config` for later steps. Each workflow grants
  least-privilege `permissions` at the top (usually `contents: read`); only `release` gets
  `contents: write`, and only `codeql` gets `security-events: write` (scoped to the job). A
  top-level `concurrency` group cancels superseded runs, except on release where
  cancel-in-progress is false so an in-flight publish is never killed mid-upload.
  Dependabot covers both `gomod` and `github-actions` weekly (grouped minor/patch) so the
  pinned SHAs do not silently rot. The coverage gate runs against a live Redis service
  container because the Redis `Store` adapter is a shipped feature that the default test
  matrix otherwise skips when no server is reachable; without it, that code had no CI signal
  at all. The Redis service plus coverage gate lifted total coverage from 68.4% to 75.6%;
  the floor is 70% via `scripts/coverage.sh`, enforced in-repo with no coverage service.

- **2026-09-25: Docs drift is enforced by a pure-Go test in `internal/docscheck`, not
  lychee/vale/markdownlint; the CI `docs` job is always-run and not path-filtered.**
  R2 recommended a link/prose linter plus a Go drift test. We implemented only the Go test:
  it has no dependency, no network, and it checks the facts that actually drift for a
  single-binary project (routes, env vars, metrics counters, version pin, relative links,
  ASCII-only). lychee/vale/markdownlint would catch broken links and style but add CI-only
  tooling, version pinning, and network flakiness for little marginal safety; the relative
  link and ASCII checks cover the parts that matter offline. The `docs` job is deliberately
  not path-filtered because branch protection requires status checks by name: a docs-only
  PR whose `docs` job never ran would wait forever on a required check. Always-run keeps the
  check truthful for every PR. The Conventional Commits `pr-title` job is likewise
  always-run-on-PR and left unrequired until proven stable. A PR template carries the
  "docs updated or no docs impact" checkbox; the test makes the rule mechanical.

- **2026-09-25: E2E and eval suites run offline against an in-process OpenAI-compatible
  stub; case data lives in a checked-in JSON file.**
  The README's central claim (any OpenAI-compatible endpoint works end to end) was only
  tested through `llm.Fake`, which bypasses the real `internal/llm` client and the HTTP
  layer. `internal/llmstub` is a test-only `httptest.Server` that speaks
  `/v1/chat/completions`, so `internal/e2e` can drive the full stack (registry -> SQLite ->
  engine -> worker pool -> `api.Handler()`) with the real OpenAI client and zero network or
  model cost. Eval cases are data, not code: `internal/eval/cases.json` is embedded and
  scored CORRECT/INCORRECT, which keeps the suite versioned and reviewable and avoids a new
  dependency. Both run in the normal `go test ./...` gate (`make test`), so they are
  required in CI without new workflow steps. The optional real-model replay is documented
  but never runs in tests.

- **2026-09-24: Phase 11: Redis is an alternate `Store` behind the same interface, with a
  stdlib-only RESP client.**
  `store.Store` is unchanged; a new `internal/store/redis` package implements it for
  deployments that want job state to outlive a single container (or be shared across
  replicas). Selection is explicit: `HARNESS_STORE=sqlite|redis`, default `sqlite`, so the
  embedded single-binary path stays the default and Phase 0-10 behavior is untouched.
  Connection is configured by `HARNESS_REDIS_ADDR` (default `127.0.0.1:6379`),
  `HARNESS_REDIS_PASSWORD`, and `HARNESS_REDIS_DB`.
  The adapter talks RESP directly over `net.Conn` instead of pulling in `go-redis`, honoring
  the "one runtime dependency" discipline (see the SQLite decision): jobs are hashes under
  `jh:job:<id>` plus per-status sets for `ListJobsByStatus`, and step results are a per-session
  list under `jh:steps:<id>`. Duplicate session ids are rejected with `SADD` on a shared
  `jh:sessions` set. A tiny mutex-guarded connection pool reuses sockets, redials on error,
  and honors context deadlines. Redis remains optional: nothing in the engine or API changed.

- **2026-09-24: Phase 10: the container is a thin wrapper; the registry is the deployment.**
  The image only packages the compiled binary plus the registry bundle (`agent-registry/`), so a
  deploy is really a registry change. Two documented rollout modes: bake the registry into the
  image at build time (immutable, promote by tag) or mount it read-only / manage it over the
  CRUD API (mutable, hot-swap). `VERSION` is passed as a build arg and stamped into
  `harness --version` so an image can be identified from `/healthz`. Compose ships healthchecks
  on both harness (`/healthz`) and ollama, and `depends_on: service_healthy` so the harness does
  not start before the model server is reachable. `.dockerignore` keeps the build context small.

- **2026-09-24: Phase 9: retries live in a client decorator; schema errors get one repair turn.**
  Retries wrap the `llm.Client` (`llm.NewRetry`), not the engine, so any code path that calls
  the model benefits and the engine stays a pure policy layer. Only transport failures and
  HTTP 429/5xx are retried (exponential backoff with jitter, default 2 retries): a 4xx like
  400/401 is a caller bug and fails fast. Retry is off when `Retry.MaxAttempts <= 1`.
  Separately, when a blueprint has `output_schema`, the parsed JSON is validated against it;
  on mismatch the engine makes **one** repair turn (feeding the validation error back) before
  failing, mirroring the tool-loop bound. Output schemas are compiled at registry load so a
  malformed schema fails fast at startup, not on the first request.
- **2026-09-24: Phase 9: `internal/schema` is a deliberately small draft-07 subset.**
  Supports `type`, `required`, `properties`, `additionalProperties` (bool), `enum`,
  `minimum`/`maximum`, `minLength`/`maxLength`, `minItems`/`maxItems`, and `items`. That is
  enough for structured agent output; pulling a full JSON-Schema library was rejected to keep
  the dependency count at one (see the SQLite decision).
- **2026-09-24: Phase 9: metrics are in-process counters exposed on `/metrics`.**
  A tiny `internal/metrics` registry of atomic counters (submissions, completions, failures,
  retries, schema failures) rendered as Prometheus text. No Prometheus client dependency; the
  endpoint is unauthenticated like `/healthz` so scrapers work without a token.

- **2026-09-24: Phase 8: tools are opt-in, allowlisted, and fail-closed.**
  Function calling is disabled unless `ENABLE_TOOLS=true` is set, in which case the process
  builds an `internal/tools.Registry` from a fixed set of built-in tools and hands it to the
  `Engine`. There is no shell, filesystem, or arbitrary-network tool; the built-ins are pure
  and side-effect-free (`current_time`, `word_count`, `math_eval`). A blueprint's `tools`
  list is only a request: if tools are disabled, or a listed tool is not in the registry, the
  run fails with a clear error rather than silently ignoring the list. Tool execution happens
  in a bounded loop (at most `maxToolRounds` = 5 model turns); each round the model may call
  tools, the engine executes them and appends `role:"tool"` messages, then re-asks. This keeps
  a misbehaving model from looping forever and keeps the RCE surface at zero for the default
  deployment (see the Phase 0 security gap: unauthenticated execution plus `run_bash`).

- **2026-09-24: Phase 7: registry CRUD writes reload the whole registry and hot-swap the engine snapshot.**
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

- **2026-09-24: Phase 6: dependencies are derived from refs, `needs`, and router edges.**
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
- **2026-09-24: Delivery track: releases, installer, Pages, branch protection.**
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
- **2026-09-24: Phase 5 (v1): pipelines run sequentially; omitting `output` means "last step".**
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

- **2026-09-24 04:10: API middleware order = recoverer -> logging -> auth -> mux.** Panic
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
  Rationale: supports sequential chains, fan-out/fan-in, and triage->route->respond without a
  special-case engine; a linear-only schema would force a rework at the first branch.

- **2026-09-24 04:00: Async by default.** All executions return `202` + `session_id`; results
  are polled via `GET /v1/sessions/{id}`. Rationale: local CPU inference can take minutes and
  would otherwise time out synchronous HTTP requests.

- **2026-09-24 04:00: Bounded worker pool, not goroutine-per-request.** Concurrency is capped
  (default 1-2). Rationale: a 2-core host cannot parallelize local inference; unbounded
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
