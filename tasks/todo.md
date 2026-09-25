# Todo

One `in_progress` item at a time. Update this file before moving on.

## In progress

- [ ] (none: all phases complete)

## Next action

- No engine phases remain. Optional follow-ups if ever wanted: streaming
  responses, a `POST /v1/sessions/{id}/cancel` endpoint, or per-tenant auth. Not
  scheduled; add a new phase to `tasks/roadmap.md` first if picked up.
- All research follow-ups from `docs/research/RECOMMENDATIONS.md` that we chose to
  take are done (R1-R7; R8 versioned docs site deferred). See D10 and D11 in
  `tasks/roadmap.md`.

## Blockers

None.

## Backlog (engine phases, in order)

- [ ] (empty)

## Done

- [x] **D12: action major bumps + Dependabot grouping** (verified: every pinned
  action SHA re-resolved against its official repo via `gh api repos/<repo>/commits/<tag>`
  and matched; `grep` confirms no old SHAs remain in `.github/workflows/`; all workflow
  YAML parses with `yaml.safe_load`; the `codeql-action` group is listed before the
  catch-all `actions` group so the first-match-wins grouping keeps `init`/`autobuild`/`analyze`
  in lockstep; full gate + CI green on the PR.)
  - `actions/checkout` v4 -> v7.0.1 (`3d3c42e...`) across ci, codeql, release, nightly, gh-pages.
  - `actions/setup-go` v5 -> v7.0.0 (`b7ad1da...`) across ci, codeql, release, nightly.
  - `github/codeql-action/{init,autobuild,analyze}` v3 -> v4.38.2 (`2892aa5...`) together.
  - `goreleaser/goreleaser-action` v6 -> v7.2.3 (`f06c13b...`).
  - Why hand-rolled instead of merging Dependabot #16-#20: Dependabot treated the three
    `github/codeql-action` subpaths as separate dependencies, opened PRs for `init` and
    `analyze` only, left `autobuild` behind, and lagged the current patch. `dependabot.yml`
    now groups `github/codeql-action*` into one PR (plus a catch-all `actions` group).

- [x] **D11: research follow-ups (golden wire tests, CODEOWNERS, semantic PR title,
  scheduled container + real-model checks)** (verified: `go test ./internal/e2e/ -run TestGolden` 6/6 pass and `UPDATE_GOLDEN=1` regeneration round-trips;
  `sh scripts/container_e2e.sh` prints `container e2e passed` against a real
  `j-harness:e2e` image with the stub on a shared Docker network; `sh -n` on both
  scripts, `python3 ast.parse` on `stub_llm.py`, `yaml.safe_load` on
  `.github/workflows/ci.yml` + `nightly.yml`; `gofmt -l .` empty; `go vet ./...`
  clean; full gate `make fmt-check vet test build` green.)
  - `internal/e2e/golden_test.go` + `internal/e2e/testdata/*.json` (new): 6 golden
    files pin the exact submit/session/steps/skipped-steps/error wire shapes;
    `session_id` replaced with a placeholder and volatile `duration_ms` (an
    `omitempty` field) dropped; regenerate with `UPDATE_GOLDEN=1`.
  - `.github/CODEOWNERS` (new): `* @bigknoxy` plus registry/docs/.github paths.
  - `.github/workflows/ci.yml`: `pr-title` now uses the pinned
    `amannn/action-semantic-pull-request` action (PR-only) instead of a shell regex.
  - `scripts/container_e2e.sh` (new, rewritten): builds the image and drives the
    async API over real HTTP; the stub runs as a container on a private Docker
    network because a host firewall drops container-to-host traffic.
  - `scripts/stub_llm.py` (new): stdlib OpenAI-compatible stub.
  - `scripts/eval_live.sh` (new): replays billing/technical triage cases against a
    real model.
  - `.github/workflows/nightly.yml` (new): scheduled container E2E + real-model
    eval (skips without `NVIDIA_API_KEY`); deliberately not a required check.
  - docs: `README.md` testing section, `docs/EVALS.md`, `docs/DOCUMENTATION.md`
    (new rows + scheduled note), `docs/MEMORY.md` decision entry, `tasks/roadmap.md` D11.

- [x] **D10: CI hardening + security (dependabot, coverage gate, pinned actions)**
  (verified: `gofmt -l .` empty; `go vet ./...` clean; `go test -race ./...` all
  packages ok; `HARNESS_REDIS_ADDR=127.0.0.1:6379 sh scripts/coverage.sh` ->
  `total coverage: 75.6%` / `coverage gate passed` against a live `redis:7-alpine`;
  fail path with `COVERAGE_THRESHOLD=99` exits 1; all workflow + dependabot YAML
  parsed by `yaml.safe_load`; no `@vN` action refs remain, every `uses:` is a
  commit SHA.)
  - `.github/workflows/ci.yml`: pinned `actions/checkout`/`actions/setup-go` to
    commit SHAs, `persist-credentials: false`, top-level `concurrency` with
    cancel-in-progress, pinned `staticcheck v0.8.1` / `govulncheck v1.8.0` (env
    vars, no more `@latest`), new always-run `coverage` job with a `redis:7-alpine`
    service container (health-checked) that runs `scripts/coverage.sh`, and a new
    PR-only `dependency-review` job (`fail-on-severity: moderate`).
  - `scripts/coverage.sh` (new): `go test -coverprofile` + `go tool cover`, integer
    x10 comparison so `69.9` correctly fails a `70` threshold; `COVERAGE_THRESHOLD`
    (default 70) and `COVERAGE_PROFILE` overridable.
  - `.github/workflows/codeql.yml` (new): CodeQL Go analysis on push/PR/weekly,
    `security-events: write` scoped to the job only.
  - `.github/dependabot.yml` (new): weekly `gomod` and `github-actions` updates,
    Conventional-Commit prefixes, minor/patch grouping.
  - `release.yml` / `gh-pages.yml`: SHA-pinned actions, `persist-credentials: false`,
    release concurrency (no cancel-in-progress), Pages concurrency.
  - `Makefile`: `coverage` target.
  - docs: `docs/DOCUMENTATION.md` workflow row, `docs/MEMORY.md` decision entry,
    `tasks/roadmap.md` D10.

- [x] **WS1: prior-art research on docs, e2e, evals, and quality gates** (verified:
  `docs/research/PRIOR-ART.md` (405 lines) and `docs/research/RECOMMENDATIONS.md`
  (136 lines) merged; 11 comparable projects surveyed (Pydantic AI, Haystack,
  inspect_evals, LangChain, Temporal, DSPy, promptfoo, Argo, OpenAI Evals); ranked
  recommendations R1-R8 with effort/impact/dependency. Drove WS3 and WS4.)
  - `docs/research/PRIOR-ART.md`: how peer projects handle doc sync, real-server
    e2e, eval suites, and quality gates
  - `docs/research/RECOMMENDATIONS.md`: gaps in j-harness plus a proposed order
    (R1 -> R6 -> R2 -> R4 -> R3 -> R5 -> R7, R8 deferred), all keeping `go.mod` at
    one runtime dependency

- [x] **WS2: full documentation sync after phases 8-11** (verified: 14 files changed
  in one PR; reviewed the full diff for factual accuracy.)
  - `index.html` rebuild (roadmap "all phases 0-11 complete", new feature cards,
    API table gains `/metrics` and registry methods)
  - README status/features/API table/install pin `v0.1.0`/DEPLOY+DOCUMENTATION links
  - AGENTS.md, docs/ARCHITECTURE.md (ASCII diagram, Redis, CPU-count workers,
    metrics no longer "planned"), docs/API.md, docs/SCHEMA.md, docs/DEPLOY.md,
    docker-compose.yml, docs/MEMORY.md, tasks/*.md
  - deleted `config.example.json` (dead: no Go loader, keys match no flag/env var)
  - new `docs/DOCUMENTATION.md` (documentation map + consistency checklist)

- [x] **WS5: code review + simplifier pass (phases 8-11)** (verified: `gofmt -l .`
  empty; `go vet ./...` clean; `go test -race ./...` all packages ok; test count
  122 -> 125.)
  - 7 real bug fixes: per-blueprint tool allowlist enforced at call time; bare
    `null` rejected by `validateJSONObject`; `repairSchema` uses the run's captured
    schema; `WriteBlueprint` compiles `output_schema` before writing; metrics map
    race fixed with `RWMutex`; constant-time bearer token compare; Redis `EXEC`
    per-command errors surfaced
  - simplifications: removed the write-only unbounded `jh:sessions` set (duplicate
    detection is already atomic via `HSETNX`); `resolveTools` takes one lock
    snapshot instead of three

- [x] **WS4 / R2+R4+R5: docs drift gate enforced on every PR** (verified:
  `go test ./internal/docscheck/...` 6 tests pass; deliberately corrupted README/API
  fixtures each fail with a file-naming diff message (stale route, missing route, stale
  env var, non-ASCII rune); `gofmt -l .` empty; `go vet ./...` clean;
  `go test -race ./...` all packages ok; `make fmt-check vet test` green;
  `make docs` green; CI YAML parsed by `yaml.safe_load`.)
  - `internal/docscheck/docscheck.go`: pure-Go, stdlib-only parsers for source routes
    (mux patterns + route comments), documented routes, env vars, metrics counters,
    version pins, and relative markdown links (with heading anchors), plus a heading
    `Slugify`; no network, no dependency
  - `internal/docscheck/docscheck_test.go`: six deterministic tests - routes in
    README+API (both directions), env vars in API+DEPLOY (both directions), metrics in
    API (both directions), README/DEPLOY version pin agreement, relative-link/anchor
    resolution, ASCII-only docs
  - `.github/workflows/ci.yml`: new always-run, non-path-filtered `docs` job
    (`go test ./internal/docscheck/...`) on push+PR; new `pr-title` Conventional
    Commits job on PR; existing lint/test/vuln/smoke untouched
  - `.github/PULL_REQUEST_TEMPLATE.md`: docs checklist that references the docs map
  - `Makefile`: `make docs` target
  - docs: DOCUMENTATION.md hard rule + automated drift-check section + package row;
    README testing section + local usage; MEMORY.md decision entry (pure Go vs
    lychee/vale/markdownlint; always-run job); roadmap D9

- [x] **R1/R3/R6: real HTTP e2e suite + deterministic eval suite** (verified:
  `gofmt -l .` empty; `go vet ./...` clean; `go test -race ./...` all packages ok
  incl. new `internal/e2e` (7 tests, ~1.4s) and `internal/eval` (9 cases, all
  CORRECT); `make fmt-check vet test` green.)
  - `internal/llmstub`: test-only OpenAI-compatible `httptest` server with scripted
    responses (ordered or selected by system prompt); no network, no model
  - `internal/e2e/e2e_test.go`: full stack over real HTTP (registry -> temp SQLite ->
    engine -> worker pool -> `api.Handler()` -> `httptest.Server`) driving the real
    `llm.NewOpenAI` (+ retry) client at the stub. Covers agent execute -> 202 ->
    poll COMPLETED + result + steps; `support_flow` routing (billing vs technical
    branches COMPLETED vs SKIPPED); `digest_flow` DAG; auth boundary (401 on
    `/v1/*`, 200 on `/healthz`/`/readyz`/`/metrics`); registry CRUD round-trip +
    400 on invalid write; metrics after jobs; 12-job concurrency smoke. Deadline
    polling (3ms interval, 5s deadline), no fixed sleeps.
  - `internal/eval` (`eval_test.go` + `cases.json`): checked-in deterministic eval
    runner with explicit CORRECT/INCORRECT scoring and a summary; cases for triage
    classification, output-schema conformance + single repair turn, fail-closed on
    unrepairable output, `support_flow` routing, and the bounded tool-call loop.
  - `docs/EVALS.md`; `make e2e` / `make eval` targets; docs/ARCHITECTURE testing
    section + package rows; README testing section + doc link; DOCUMENTATION map.

- [x] **Phase 11: Optional Redis `Store` adapter**
  (verified: `go test -race ./...` all packages ok incl. new
  `internal/store/redis` (1.04s); `internal/store/redis/redis_test.go` runs the full
  `Store` round-trip against a real Redis 7 (`durable across restart`, `SetJobStatus`
  moves jobs between per-status sets, `ErrNotFound` on missing, duplicate-create
  rejection); live end-to-end run with `HARNESS_STORE=redis`: submitted a `triage`
  job, polled it to `COMPLETED` with step results, then killed + restarted the
  process and the session was still readable; `make fmt-check vet test build` +
  smoke green.)
  - `internal/store/store.go`: interface unchanged; SQLite stays the default
  - `internal/store/redis`: `RedisStore` implementing `store.Store`, selected via
    `HARNESS_STORE=sqlite|redis`; `HARNESS_REDIS_ADDR`/`HARNESS_REDIS_PASSWORD`/
    `HARNESS_REDIS_DB`/`HARNESS_REDIS_PREFIX` config
  - `internal/store/redis/client.go`: small stdlib-only RESP client with a pooled
    connection, MULTI/EXEC transactions, and context deadlines (no new dependency)
  - `cmd/harness`: `--store` flag + `openStore` backends (`--db` still used for SQLite)
  - docs: README status/roadmap/feature, docs/DEPLOY env table + job-store section,
    docs/ARCHITECTURE package row

- [x] **Phase 10: Docker + compose + GitOps deploy docs**
  (verified: `docker build --build-arg VERSION=0.1.0 -t j-harness:test .` succeeded;
  `docker run --rm j-harness:test --version` -> `j-harness 0.1.0`; booted container served
  `/healthz {"status":"ok","version":"0.1.0"}` and `/metrics` counters; image ~20MB.
  Full gate + smoke green.)
  - `.dockerignore` added so the build context is small and reproducible
  - `Dockerfile`: `ARG VERSION` stamped into `harness --version` via ldflags
  - `docker-compose.yml`: harness + ollama with healthchecks, `depends_on: service_healthy`,
    registry mounted read-only, `./data` volume, pinned-image option, configurable env
  - `docs/DEPLOY.md`: image build, compose, full env table, auth note, GitOps
    "the registry is the deployment" model (promote by tag; baked vs mounted), operating
    notes (SQLite WAL backup, orphan requeue, `/metrics`, logs), hardening checklist

- [x] **Phase 9: Hardening (retries / backoff, output-schema validation, metrics)**
  (verified: `make fmt-check vet test build` passed; smoke green incl. a `GET /metrics`
  assertion; `internal/schema/schema_test.go`, `internal/llm/retry_test.go`,
  `internal/metrics/metrics_test.go`, and extended `internal/engine/engine_test.go`
  repair tests all pass)
  - `internal/schema`: small draft-07 subset validator; registry compiles the referenced
    `output_schema` at load (malformed schema fails startup; requires `output_format: "json"`)
  - `internal/engine`: JSON output validated against the schema with exactly ONE repair turn,
    then fails; increments `harness_schema_failures_total`
  - `internal/llm`: typed `HTTPError` + `NewRetry` decorator; retries transport errors and
    HTTP 429/5xx with exponential backoff + jitter, 4xx fails fast (`HARNESS_RETRIES`, default 3)
  - `internal/metrics`: in-process Prometheus-text counters on unauthenticated `GET /metrics`
  - docs: README/docs/API (health + counters + Environment tables)/docs/ARCHITECTURE
    (`## Hardening`)/docs/SCHEMA; tasks/lessons.md nil-receiver lesson

- [x] **Phase 8: Tools / function calling (gated)** (verified: `go build ./...` clean;
  `gofmt -l internal cmd` empty; `go test -race ./...` all packages ok;
  `internal/tools/tools_test.go` covers the arithmetic evaluator + registry;
  `internal/engine/tools_test.go` covers tool round-trip, fail-closed when disabled,
  unknown-tool rejection, no-tools passthrough, and the bounded loop)
  - `internal/tools`: fixed built-in allowlist (`current_time`, `word_count`, `math_eval`);
    pure, side-effect-free, no shell/fs/network; own recursive-descent arithmetic evaluator
  - `internal/llm`: `Message` gained `ToolCalls`/`ToolCallID`; `Request` gained `Messages`
    + `Tools`; `Response` gained `ToolCalls`; OpenAI client forwards tools
  - `internal/engine/tools.go`: `resolveTools` (fail-closed) + `completeWithTools`
    (bounded `maxToolRounds = 5`)
  - `internal/registry`: unknown tool names rejected at load
  - `cmd/harness`: `ENABLE_TOOLS=true` builds the allowlist and calls `eng.SetTools`
  - `agent-registry/blueprints/calculator.json` + prompt: checked-in tool example

- [x] **Phase 7: Registry CRUD API** (verified: `make fmt-check vet test build` passed;
  smoke green; `internal/api/registry_test.go` covers list/get/create/update, id-mismatch
  and invalid payload rejection, unknown-field rejection, 409 on duplicate create, 404 on
  PUT-missing, pipeline CRUD, 405 + Allow, and auth-gating. `internal/engine/registry_swap_test.go`
  covers snapshot swap + nil no-op.)
  - `internal/api/registry.go`: `GET|POST|PUT /v1/registry/{agents,pipelines}[/{id}]`;
    strict JSON bodies; create=409 on existing, update=404 on missing
  - `internal/api/api.go`: writes go through `mutate` -> validate -> atomic write ->
    whole-registry `Load` -> hot-swap API + engine snapshots (serialized by `mu`)
  - `internal/engine/engine.go`: registry snapshot behind `sync.RWMutex` + `SetRegistry`;
    `RunAgent`/`RunPipeline` read via `getRegistry` (in-flight runs keep their snapshot)
  - docs/API.md: registry management section + `409` status code

- [x] **Phase 6: Parallel DAG fan-out/fan-in** (verified: `make fmt-check vet test build`
  passed; smoke green; `internal/pipeline/graph_test.go` covers refs/needs/router edges,
  cycles, unknown refs. `internal/engine/dag_test.go` proves concurrent fan-out with a
  barrier client and stops on failure. Registry tests cover forward-ref-allowed + cycle reject.)
  - `internal/pipeline/graph.go`: `Deps` unions template refs + `needs` + router gotos, rejects
    unknown edges and cycles at load time
  - `internal/model`: `Step.Needs` + `StatusSkipped`
  - `internal/engine/pipeline.go`: concurrent DAG scheduler bounded by `GOMAXPROCS`; failed or
    skipped predecessors skip their dependents
  - `internal/registry`: forward refs allowed; cycles/unknown needs rejected
  - `agent-registry/pipelines/digest_flow.json`: fan-out (`summarize` + `assess_risk`) -> join
    (`combine` via `needs`)
- [x] **D8: Dogfood: real runs via a hosted OpenAI-compatible provider**
  (verified: ran the checked-in registry through the async API against NVIDIA NIM
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. `triage` classified billing -> high and a
  crash -> technical; `support_flow` routed correctly to `billing_reply` / `tech_reply` with
  the other branches `SKIPPED`. Homelab Ollama was saturated, so it was not used. Notes in
  `docs/MEMORY.md`.)
- [x] **Delivery track D1-D7** (branch protection, CI, GoReleaser releases, installer +
  uninstaller, Pages site, repo polish, README badges). v0.1.0 release published with
  linux/darwin/windows amd64+arm64 assets; one-liner install/uninstall verified end to end.
- [x] **Phase 0: Bootstrap** (CI + Pages green; live at https://bigknoxy.github.io/j-harness/)
- [x] **Phase 1: Registry** (load/validate/atomic-write + tests)
- [x] **Phase 2: LLM client + single-agent execution**
- [x] **Phase 3: HTTP API (sync) + middleware**
- [x] **Phase 4: SQLite store + async jobs + bounded worker pool + orphan requeue**
  (verified: `make fmt-check vet test build` passed; smoke green; store round-trip,
  worker lifecycle/requeue/queue-full, async API success/failure/auth/404/405 covered.
  Go toolchain/Dockerfile/CI now track current stable 1.27; pure-Go `modernc.org/sqlite`
  behind a `Store` interface. See `docs/MEMORY.md`.)
- [x] **Phase 5: Sequential pipeline + router (v1)** (verified: `make fmt-check vet test build`
  passed; smoke green; resolver/condition/router tests, engine pipeline tests, worker pipeline
  test, API pipeline submit + steps tests)
  - `internal/pipeline/resolve.go`: runtime `Resolve(scope)` for `{{ inputs.x }}` and
    `{{ steps.<id>.output[.json.path] }}` (JSON path incl. array indices)
  - `internal/pipeline/condition.go`: `EvaluateCondition` + `PickRoute` (equals/not_equals/
    in/matches/exists) with default fallback
  - `internal/engine/pipeline.go`: `RunPipeline` walks steps in order, router selects one
    successor and marks others `SKIPPED`, result = last agent step when `output` is omitted
  - `internal/engine/worker.go`: `KindPipeline` jobs execute + persist every step outcome
  - `internal/api`: `POST /v1/pipelines/{id}/execute` -> `202`, strict named-input body
  - `agent-registry/pipelines/support_flow.json`: top-level `output` removed (branch-safe)

## Notes / working memory

- v1 = Phase 5 (sequential pipeline); Phase 6 added DAG fan-out/fan-in.
- State = embedded SQLite behind a `Store` interface by default; Redis is an opt-in
  adapter (`HARNESS_STORE=redis`) shipped in Phase 11.
- Worker pool is **bounded** (2-core host; local inference serializes); DAG fan-out is
  bounded by `GOMAXPROCS`.
- Step dependencies come from `{{ steps.<id>.output }}` refs + explicit `needs` + router
  gotos; cycles are rejected at load. `{{ steps.<id>.output }}` keys off the **step id**.
- Pages gotcha: no `.nojekyll` (breaks README rendering); see `tasks/lessons.md`.
- Dogfood backends: Ollama `http://192.168.8.136:11434` (`qwen3:8b`); NVIDIA NIM
  `https://integrate.api.nvidia.com/v1` (key in `/root/.bashrc`), working model
  `nvidia/nemotron-3-nano-omni-30b-a3b-reasoning`. Do not overload the homelab.
- Full plan + rationale: see `docs/MEMORY.md` and `docs/ARCHITECTURE.md`.
