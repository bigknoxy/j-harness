# Prior art: docs freshness, E2E tests, LLM evals, and quality gates

Research brief for `j-harness`. Written 2026-09-25 in worktree `research/harness-practices`.

## Scope

Four questions, applied to comparable projects:

- (a) What is the source of truth for docs, and how is documentation kept in sync with code?
- (b) How are end-to-end (E2E) tests structured (real servers vs mocks, services, golden files)?
- (c) How are LLM eval suites structured (dataset format, scoring, regression thresholds, CI)?
- (d) What code-quality gates are enforced, and how (lint, static analysis, coverage, required checks)?

Comparable sets examined:

- Agent / LLM frameworks: LangChain, LlamaIndex, Haystack, Semantic Kernel, Pydantic AI, CrewAI, AutoGen/AG2, DSPy.
- LLM eval / testing: promptfoo, OpenAI Evals, DeepEval, Ragas, Inspect (UK AISI) + inspect_evals, lm-evaluation-harness. Braintrust and LangSmith are proprietary hosted platforms and were not analyzed from primary repo sources in this pass (marked uncertain below).
- Config-driven / pipeline tools: Dagger, Dagster, Prefect, Temporal Go SDK, Argo Workflows.
- Go OSS exemplars: Caddy, Cobra, Prometheus, golangci-lint, plus testcontainers-go for E2E services.

## Method

- Primary sources only. Claims come from CI workflow files, Makefiles, repo docs, and official documentation, read through the GitHub API (`gh api ... contents/...`) on 2026-09-25.
- Workflow files were read directly, not inferred from blog posts. Where a repo's practice is only implied by a file name, the claim is marked uncertain.
- No product feature is asserted from memory. Proprietary SaaS eval platforms were excluded rather than guessed at.
- URLs are in Sources. File paths are given per claim so a reader can re-fetch.
- Time-box: this is a research pass, not an audit. Not every workflow in every repo was read; coverage is called out per finding.

## Findings by category

### A. Documentation source of truth and freshness enforcement

**LangChain splits docs from code and treats generated output as build artifacts.**
`langchain-ai/docs` is a dedicated repo. Source lives in `src/` (`.md`/`.mdx`); Mintlify
publishes from `build/`; the README states "Never edit `/build` directly." API reference is
generated and deployed outside the repo (`reference.langchain.com`). The repo carries
`.markdownlint.json`, `.vale.ini`, and `.pre-commit-config.yaml`. `make` targets include
`broken-links`, `broken-links-with-anchors`, `lint_md`, `lint_prose` (Vale, pinned in
`.mise.toml` so local and CI match), and codespell. CI workflows include `_check-links.yml`
(Mintlify link checker), `lint-prose.yml` (Vale on changed files only), `test-code-samples.yml`
(runs docs samples, with a Postgres service), `check-removed-pages-redirects.yml`,
`check-version-claims.yml`, and `check-external-doc-links.yml`. The main
`langchain-ai/langchain` repo has `pr_lint.yml` (Conventional Commits PR titles via
`amannn/action-semantic-pull-request`), `check_diffs.yml` (changed-package test matrix), and
`_lint.yml` running `make lint_package` / `make lint_tests` through ruff. Docs are versioned
per release (documented at docs.langchain.com/oss/python/versioning).

**Haystack generates its API reference and then self-merges the sync PR.**
`check_api_ref.yml` hashes docstrings across `haystack/**/*.py` on the base and PR trees and
only rebuilds the API reference if they differ. `docusaurus_sync.yml` regenerates the
reference and `rsync --delete`s it into `docs-website/reference/haystack-api`, then opens a PR.
`auto_approve_api_ref_sync.yml` approves and auto-merges those PRs, but only when the author is
`HaystackBot`, the branch is `sync-docusaurus-api-reference*`, and the head repo equals the base
repo. `docs-website-test-docs-snippets.yml` runs daily to execute Python snippets in the docs.
This is the strongest generated-docs pattern in the sample: generation, drift detection, and a
narrowly-scoped automated merge.

**Pydantic AI separates fast offline link checks on PRs from weekly external sweeps.**
`ci.yml` has a `docs-assets` job that runs `lychee` with `--offline --include-fragments` over
`./docs/**/*.md`, `./*.md`, `./examples/**/*.md`, so PRs only validate local links and anchors
with no network flakiness. `link-check.yml` runs weekly, checks external links, uses
`fail: false`, and opens an issue from the report when the sweep fails. `lychee.toml` accepts
403/429 (bot-blocking), sets a browser-like user agent, retries, and excludes login-gated hosts.
A separate `docs-only` job checks Markdown and runs `pytest tests/test_examples.py` to execute
doc snippets. There is also a weekly agentic `pydantic-ai-docs-drift.md` workflow that detects
"negative docs drift" and files one issue, and a label-triggered `docs-navigation.yml` that
dispatches navigation validation to a separate docs repo without executing PR code.

**Prefect, Dagster, and DSPy use small, targeted docs gates.**
Prefect's `docs-broken-links.yaml` runs `just links` on `docs/**` changes; it also has
`markdown-tests.yaml` and `static-analysis.yaml` running pre-commit. Dagster's `check-docs.yml`
runs `docs/scripts/check-frontmatter.py` on `docs/**` PRs. DSPy's `docs-push.yml` builds the
MkDocs site on PRs (`python docs/scripts/build_docs.py current`) and publishes only on `main`.

**inspect_evals enforces generated-file drift with `git status` and a custom linter.**
`checks.yml` runs `tools/generate_readmes.py` and `tools/generate_asset_manifest.py`, formats
the output, then fails if `git status --porcelain` is non-empty ("Generated files are out of
date"). It also runs markdownlint, actionlint, and a workflow-security linter. A custom
`inspect-evals-lint` package encodes eval structure rules (file layout, registration, tests,
dataset pinning) with rule codes and a suppression syntax; CI annotates the offending lines.

**Argo fails docs CI if the docs build mutates tracked files.**
`docs.yaml` runs `make docs` then `git diff --exit-code`, so a linter or generator that changes
files fails the build. `pr-readiness.yaml` posts a single sticky reminder comment and applies a
`bot-not-ready` label based on completed CI workflows, without ever checking out PR-head code.

**Other frameworks, less granular.** LlamaIndex has `sync-docs.yml` and `coverage_check.yml`.
CrewAI has `docs-broken-links.yml`, `type-checker.yml`, and `vulnerability-scan.yml`. Semantic
Kernel uses `markdown-link-check.yml` with `umbrelladocs/action-linkspector`, plus `typos.yaml`
and `merge-gatekeeper.yml`. Ragas has an agentic `claude-docs-check.yml` that asks a model
whether a code-only PR needs docs and returns a small JSON verdict. These are noted from
workflow listings; internals were not all read.

### B. End-to-end test approach

**Real servers in CI are common, but isolated from the fast unit suite.**
Haystack's `e2e.yml` runs on a nightly schedule and on PRs that touch `e2e/**`, uses a real
`OPENAI_API_KEY` secret, and has a 60-minute timeout, with a Slack alert on nightly failure.
Temporal's Go SDK runs `go run . integration-test -dev-server` against a real (downloaded) dev
server, with a `wait_for_server.go` helper that polls `client.Dial` for up to five minutes; it
runs both with and without the workflow cache, across an OS/Go matrix, and uploads full test
logs as artifacts on `always()`. The unit job in the same workflow has no server. This separation
(unit fast and offline; integration real and slow) is the dominant pattern.

**Language/framework repos use service containers and real providers.**
LangChain's docs `test-code-samples.yml` declares a GitHub Actions `services:` Postgres
(`pgvector/pgvector:pg17`) with a health check, runs only the changed samples on PRs, and runs
everything monthly on a schedule with a 90-minute timeout. DSPy's "Run Tests with Real LM" job
boots Ollama in Docker with a cached model, warms it with three retries, then runs
`pytest -m llm_call` against `ollama/llama3.2:3b`. Semantic Kernel's
`python-integration-tests.yml` runs daily at midnight against Azure OpenAI, Postgres, Mongo,
and more, with a large secret surface.

**Go projects favor the standard library and real processes.**
Caddy's `ci.yml` builds the binary and runs a smoke test before `go test -coverprofile=...
-short -race ./...`; integration-ish checks run separately. Prometheus runs many tagged test
slices (32-bit, dedupelabels, `compliance`) and uses `git diff --exit-code` after codegen to
detect generated-file drift. Temporal is the clearest Go "real server, poll until ready"
example. testcontainers-go runs Go tests against real Docker, including a rootless-Docker
variant, and wires results to SonarQube.

**Golden files and mocks.** LangChain has `_test_vcr.yml` (VCR cassettes) as a recorded-HTTP
pattern, and `_compile_integration_test.yml` as a compile-only check. Go projects generally use
`httptest` servers and table tests rather than golden files at scale. This brief did not find a
dominant golden-file convention in the sample.

### C. LLM eval suites

**promptfoo is the most CI-native, config-driven eval tool in the sample.**
A `promptfooconfig.yaml` declares `prompts`, `providers`, and `tests` (each with `vars` and
`assert`). Assertions cover exact match, similarity, JSON structure, and model-graded scoring,
with a published config schema (`https://promptfoo.dev/config-schema.json`). The official
GitHub Action (`promptfoo/promptfoo-action@v1`) watches paths such as `prompts/**`, runs a
before/after comparison on the PR, and posts the result with a link to the web viewer; prompts
are provided as file globs. Cost is controlled with an `actions/cache` entry for
`~/.cache/promptfoo`. The repo also gates its own coverage with an in-repo ratchet
(`test:coverage:ratchet`) rather than only a hosted coverage service.

**Inspect is the most rigorous about eval correctness and anti-flake structure.**
`inspect_ai` is the framework; `inspect_evals` is the benchmark registry. Each eval declares
metadata in `eval.yaml` validated by a model (unknown keys fail), and inspect_evals requires at
least one E2E test that uses the `mockllm/model` provider so evals are deterministic in CI.
Custom `@solver`, `@scorer`, and `@tool` functions must have tests. Scoring uses explicit
`CORRECT`/`INCORRECT` constants; dataset loading requires a `revision=` pin. Contributors are
asked to manually verify each eval end to end on a sample (`inspect eval ... --limit 10`) and to
version tasks when results could change. A custom linter (`inspect-evals-lint`) encodes these
rules with codes (IEFS/IECQ/IETS/IEBP) and suppression comments, and CI annotates PRs.

**OpenAI Evals is a registry plus templates, not a CI gate.**
Evals are YAML definitions over JSONL data stored with Git-LFS; contributors can add model-graded
evals without writing code. It documents `run-evals.md`, `eval-templates.md`, and a completion
function protocol. The repo uses pre-commit and `mypy.ini`, and has `tests/unit/evals`, but the
README does not describe regression-threshold CI gating; the current guidance points to the
hosted dashboard. Treat "CI gating" as not established for this project.

**DeepEval makes LLM testing look like pytest.** Tests are split into core/metrics/integration/
model-registry/templates workflows, run with `OPENAI_API_KEY` and telemetry opt-out, and there is
a maintainer-only `full_test_core_for_pr.yml` (`workflow_dispatch`, 60-minute timeout) for full
runs on a PR ref. Ragas runs a Python/OS test matrix with `paths-filter` to skip unrelated PRs.
lm-evaluation-harness task YAML declares `metric_list` (e.g. `acc`, `acc_norm`),
`aggregation`, `higher_is_better`, and `metadata.version`; `new_tasks.yml` detects changed task
folders and runs the affected tests.

**Uncertain.** Braintrust, LangSmith, and similar hosted platforms are proprietary; their
internals and public docs were not read here. Claims about their scoring or CI gating are
deliberately omitted rather than guessed.

### D. Code-quality gates

**j-harness's Go peers converge on: format check, vet, a static analyzer, race tests on a
matrix, and a vulnerability scan.** Caddy's `lint.yml` runs `golangci-lint-action` across
Linux/macOS/Windows and pins actions by SHA; `ci.yml` runs build, a smoke test, then
`go test -race -short ./...`. Cobra runs `golangci-lint-action`, a license-header check via
`ghcr.io/google/addlicense`, and a wide Go-version matrix. Prometheus uses `make test`,
`govulncheck`, CodeQL, fuzzing, 32-bit tests, and `git diff --exit-code` after codegen.
The `cobra` and `prometheus` repos both include a stable-named, always-passing "empty" job
pattern specifically so branch protection rules do not deadlock on skipped workflows.

**Framework repos add PR-hygiene and coverage policy.** LangChain enforces Conventional Commits
PR titles and runs tests only for changed packages via a generated matrix (`check_diff.py`).
Haystack runs ruff format-check, a license-header check in Docker, an import checker, and a
`paths-filter` so docs-only PRs do not run tests, while guaranteeing a "Mark tests as completed"
job still runs for branch protection. promptfoo runs Biome, Prettier, `knip` (unused files),
`madge --circular` (circular deps), an architecture-boundary check, and `lockfile-lint`.
inspect_evals pins the linter version in both `pyproject.toml` and `.pre-commit-config.yaml`.
DSPy lints its own workflows with `actionlint` and `zizmor`.

**Recurring anti-patterns to avoid.** `@latest` tool installs in CI (non-reproducible),
coverage services used as the only gate (informational, not enforcing), and SaaS eval platforms
as the only regression signal (creates a network/cost dependency for a pure-Go project).

## Comparison tables

### Docs source of truth and drift control

| Project | Source of truth | Drift mechanism | Versioned docs |
|---|---|---|---|
| LangChain | `src/` in separate docs repo | markdownlint, Vale, Mintlify broken-links, codespell, snippet tests | Yes |
| Haystack | docstrings in code | docstring checksum, generated ref, auto-merged sync PR | vX.Y docs site |
| Pydantic AI | `docs/**/*.md` | lychee offline+anchors on PR, weekly external sweep, snippet pytest, docs-drift agent | Via unified docs |
| Prefect | `docs/**` | `just links`, pre-commit, markdown tests | Yes |
| Dagster | `docs/**` | frontmatter script | Yes |
| DSPy | `docs/**` MkDocs | docs build on PR | current + versioned |
| inspect_evals | generated READMEs | `git status` drift check, custom linter, markdownlint | n/a |
| Argo | generated site | `git diff --exit-code` after `make docs` | Yes |
| j-harness (today) | `README.md` + `docs/*.md`, hand-written | None; AGENTS.md policy only | No |

### E2E / integration test shape

| Project | Unit layer | Integration layer | External services |
|---|---|---|---|
| Haystack | pytest, no network | `e2e/` nightly + path-triggered, real API key | OpenAI API |
| Temporal Go SDK | `go test` matrix | `integration-test -dev-server`, poll-until-ready, log artifacts | Real dev server |
| LangChain docs | unit tests | changed snippets on PR, all monthly | Postgres service, providers |
| DSPy | pytest matrix | `llm_call` marked tests | Ollama in Docker |
| Semantic Kernel | unit workflows | daily integration | Azure OpenAI, Postgres, Mongo |
| Caddy | `go test -race -short` | build + smoke, platform matrix | none (local binary) |
| j-harness (today) | `go test -race ./...` with `llm.Fake` | `scripts/smoke.sh` (boot + healthz/metrics/404) | none |

### Eval suite shape

| Tool / project | Case format | Scoring | CI gating |
|---|---|---|---|
| promptfoo | YAML config + asserts | exact/similarity/JSON/model-graded | GitHub Action before/after comment |
| OpenAI Evals | YAML + JSONL (LFS) | templates + model-graded | Not established in repo |
| Inspect / inspect_evals | Python tasks + `eval.yaml` | explicit CORRECT/INCORRECT, metrics | `mockllm` E2E tests + custom linter |
| DeepEval | pytest tests | LLM metrics | Core/metrics/integration workflows |
| lm-eval-harness | task YAML | `metric_list` + aggregation | changed-task test job |
| j-harness (today) | none | none | none |

### Quality gates

| Project | Format/vet | Static analysis | Coverage | Vuln/security | PR hygiene |
|---|---|---|---|---|---|
| Caddy | yes | golangci-lint (3 OS) | coverage profile (upload optional) | harden-runner, scorecard | - |
| Cobra | yes | golangci-lint | - | addlicense | - |
| Prometheus | yes | make test, codegen diff | - | govulncheck, CodeQL, fuzzing | release-notes check |
| LangChain | ruff format | ruff/mypy per package | Codecov + comment | - | Conventional Commits title |
| Haystack | ruff | mypy, import check | coverage comment | license, scorecard | paths-filter + required job |
| promptfoo | Biome, Prettier | knip, madge, architecture, lockfile-lint | in-repo ratchet | code scanning | PR title validation |
| j-harness (today) | gofmt, go vet | staticcheck (`@latest`) | none | govulncheck | none |

## What good looks like

1. Docs are a build artifact with a drift check. Either content is generated from code (Haystack
   API reference; inspect_evals READMEs) and CI fails if regeneration changes files, or docs are
   hand-written and linted/link-checked (LangChain, Pydantic AI). The weakest position is neither.
2. Link checking is split by cost. Fast, offline, anchor-aware checks run on every PR; slow
   external sweeps run on a schedule and file an issue instead of blocking merges.
3. Generated reference is verified by comparing to the committed copy, not just by rebuilding it.
4. E2E tests run the real binary or a real server, use poll-until-ready rather than sleeps, and
   are separated from the fast unit suite by path filters or schedules.
5. LLM evals are deterministic first. inspect_evals mandates a `mockllm` E2E test; promptfoo
   caches provider calls. Real-model runs are scheduled or opt-in and have cost controls.
6. Quality gates are reproducible: tool versions pinned, actions pinned by SHA, and at least one
   stable-named job that always reports so branch protection cannot deadlock.
7. Coverage is enforced in-repo (a ratchet or threshold) rather than only displayed.
8. PR hygiene is automated, not requested: title format, docs checklist, and a single readiness
   summary.

## Recommendations

Ranked by impact per unit of effort. Details and a rollout order are in
`docs/research/RECOMMENDATIONS.md`. Dependency column: none, CI-only, or runtime.

### R1. Add a real HTTP E2E test with a stub OpenAI-compatible server (High impact, S)

j-harness's core claim is "targets any OpenAI-compatible endpoint." Prove it in-process with a
stdlib `httptest.Server` that speaks `/v1/chat/completions`, then drive the actual API surface
(registry load -> enqueue -> worker -> LLM client -> store -> poll). No network, no cost, no new
dependency. This closes the gap between `llm.Fake` unit tests and `smoke.sh` (which never calls
a model). Files: new `internal/e2e` test or a `cmd/harness` test; extend `scripts/smoke.sh` to
optionally point `--base-url` at a stub. Effort S, impact High, dependency none.

### R2. Make docs a tested artifact: link/markdown lint plus a routes/env drift test (High, M)

- Add `lychee` (CI-only) over `README.md` and `docs/**/*.md`, offline + fragments on PRs.
- Add `markdownlint-cli2` (CI-only) with a small ruleset matching existing style.
- Add a pure-Go test that parses `docs/API.md` and asserts every registered route and every
  documented env var matches the code (register routes in one table; env vars in one slice).
  This is the highest-leverage freshness mechanism for a small Go repo: no external service, and
  it fails the build when docs drift. Effort M, impact High, dependency: CI-only for the
  linters, none for the Go test.

### R3. Add a checked-in eval suite, deterministic first, scheduled real-model second (High, M)

Create `evals/` with a JSONL dataset and YAML/JSON cases (expected labels, JSON-schema
conformance, tool-call shape). Add a small stdlib Go runner used as a normal `go test` against a
scripted fake client (deterministic, free). Add a nightly GitHub Actions job (DSPy pattern) that
runs the same cases against Ollama via the existing `docker-compose.yml`, caches the model, and
warms it with retries. Keep promptfoo optional as a CI-only before/after comment if richer
assertions are wanted. Effort M, impact High, dependency: none (Go runner), CI-only (optional
promptfoo), no runtime dependency.

### R4. Pin CI tool versions and add a coverage gate (Medium, S)

Replace `@latest` installs of `staticcheck` and `govulncheck` with pinned versions so CI is
reproducible. Add `go test -coverprofile` and a modest `-coverpkg`/threshold check in the
Makefile; enforce in CI without adding a hosted coverage service. Effort S, impact Medium,
dependency none.

### R5. Add PR hygiene and branch-protection safety (Medium, S)

Conventional Commits PR title check (`amannn/action-semantic-pull-request`), a `CODEOWNERS`, and
a PR template with a docs checklist. Add one stable-named, always-passing "docs-only" job so
docs-only PRs do not deadlock required checks (Haystack/Prometheus pattern). Effort S, impact
Medium, dependency CI-only.

### R6. Golden-file tests for the public wire format (Medium, S)

Store canonical JSON for `GET /v1/sessions/{id}` (completed, failed, pipeline with skipped
branches) and diff against it. This makes API changes deliberate and gives reviewers a cheap
regression signal for the format the README documents. Effort S, impact Medium, dependency none.

### R7. Docker-compose E2E on a schedule or manual dispatch (Medium, M)

Run `docker compose up` (harness + ollama), submit a checked-in agent, poll to completion, tear
down. Nightly/manual only, separate from required checks, to avoid cost and flakiness. Reuses
the Phase 10 compose. Effort M, impact Medium, dependency none.

### R8. Docs site generation and versioning (Low now, M)

The repo currently ships a hand-written `index.html`. Consider generating a docs site from
`docs/*.md` (MkDocs Material or Astro Starlight) only when the public API stabilizes; versioned
docs add real maintenance cost and are premature at v0.1.0. Defer. Effort M, impact Low now,
dependency CI-only.

### Explicitly not recommended

- Adding a runtime dependency for eval, link checking, or docs linting. All of the above can be
  CI-only or stdlib.
- A hosted coverage or eval SaaS as the primary gate. j-harness's value is a single pure-Go
  binary; external gates make local reproduction harder.
- Generated API reference before the HTTP surface is stable. A drift test (R2) gives most of the
  benefit at a fraction of the cost.

## Sources

Primary sources read on 2026-09-25 via the GitHub API.

- LangChain docs repo: https://github.com/langchain-ai/docs (README, `.markdownlint.json`,
  `.github/workflows/_check-links.yml`, `lint-prose.yml`, `test-code-samples.yml`)
- LangChain main repo: https://github.com/langchain-ai/langchain
  (`.github/workflows/pr_lint.yml`, `check_diffs.yml`, `_lint.yml`, `.markdownlint.json`)
- LangChain versioning: https://docs.langchain.com/oss/python/versioning
- LlamaIndex: https://github.com/run-llama/llama_index (workflow listing: `sync-docs.yml`,
  `coverage_check.yml`, `lint.yml`, `unit_test.yml`)
- Haystack: https://github.com/deepset-ai/haystack
  (`check_api_ref.yml`, `docusaurus_sync.yml`, `auto_approve_api_ref_sync.yml`, `e2e.yml`,
  `tests.yml`, `docs-website-test-docs-snippets.yml`)
- Semantic Kernel: https://github.com/microsoft/semantic-kernel
  (`markdown-link-check.yml`, `python-integration-tests.yml`, `python-lint.yml`, `typos.yaml`,
  `merge-gatekeeper.yml`)
- Pydantic AI: https://github.com/pydantic/pydantic-ai
  (`ci.yml`, `link-check.yml`, `lychee.toml`, `docs-navigation.yml`,
  `pydantic-ai-docs-drift.md`, `harness-compat.yml`)
- CrewAI: https://github.com/crewAIInc/crewAI (workflow listing: `docs-broken-links.yml`,
  `linter.yml`, `type-checker.yml`, `tests.yml`, `vulnerability-scan.yml`)
- AutoGen: https://github.com/microsoft/autogen (`docs.yml`, `integration.yml`, `checks.yml`)
- DSPy: https://github.com/stanfordnlp/dspy
  (`run_tests.yml`, `precommits_check.yml`, `docs-push.yml`)
- promptfoo: https://github.com/promptfoo/promptfoo (`package.json`, `.github/workflows/main.yml`,
  `site/docs/configuration/expected-outputs/index.md`, examples)
- promptfoo GitHub Action docs: https://www.promptfoo.dev/docs/integrations/github-action/
- OpenAI Evals: https://github.com/openai/evals (README, `docs/`, `tests/unit/evals`)
- DeepEval: https://github.com/confident-ai/deepeval
  (`test_core.yml`, `full_test_core_for_pr.yml`, `pr-title-check.yml`)
- Ragas: https://github.com/explodinggradients/ragas
  (`ci.yaml`, `claude-docs-check.yml`, `claude-docs-apply.yml`)
- Inspect AI: https://github.com/UKGovernmentBEIS/inspect_ai (`Makefile`, `test.yml`, `pr-gate.yml`)
- Inspect Evals: https://github.com/UKGovernmentBEIS/inspect_evals
  (`checks.yml`, `AUTOMATED_CHECKS.md`, `CONTRIBUTING.md`, `Makefile`)
- lm-evaluation-harness: https://github.com/EleutherAI/lm-evaluation-harness
  (`unit_tests.yml`, `new_tasks.yml`, `lm_eval/tasks/arc/arc_easy.yaml`)
- Dagger: https://github.com/dagger/dagger (`evals/`, `e2e/`, `.golangci.yml`,
  `.markdownlint.yaml`, workflows: `checks.yml`, `deploy-docs.yml`, `changelog.yml`)
- Dagster: https://github.com/dagster-io/dagster (`check-docs.yml`, `build-docs.yml`)
- Prefect: https://github.com/PrefectHQ/prefect
  (`docs-broken-links.yaml`, `markdown-tests.yaml`, `static-analysis.yaml`)
- Temporal Go SDK: https://github.com/temporalio/sdk-go
  (`ci.yml`, `wait_for_server.go`, `govulncheck.yml`)
- Argo Workflows: https://github.com/argoproj/argo-workflows
  (`docs.yaml`, `pr-readiness.yaml`, `pr.yaml`, `sdks.yaml`)
- Caddy: https://github.com/caddyserver/caddy (`ci.yml`, `lint.yml`)
- Cobra: https://github.com/spf13/cobra (`test.yml`)
- Prometheus: https://github.com/prometheus/prometheus (`ci.yml`, `govulncheck.yml`,
  `fuzzing.yml`, `check_release_notes.yml`)
- golangci-lint: https://github.com/golangci/golangci-lint
  (`pr-checks.yml`, `pr-tests.yml`, `pr-documentation.yml`, `new-linter-checklist.yml`)
- testcontainers-go: https://github.com/testcontainers/testcontainers-go (`ci-test-go.yml`,
  `ci-lint-go.yml`, `ci.yml`)
- Langfuse (eval/observability, reference only): https://github.com/langfuse/langfuse
  (`ci.yml.template`, `openapi-export-check.yml`, `codespell.yml`)
- lychee link checker: https://github.com/lycheeverse/lychee-action
- Vale prose linter: https://vale.sh/
- markdownlint: https://github.com/DavidAnson/markdownlint

### Uncertain or not verified

- Braintrust and LangSmith: proprietary hosted platforms, not analyzed from primary sources.
- "CI gating" for OpenAI Evals: the repo documents running evals and a dashboard, but no
  regression-threshold gate was found in the README or workflows.
- AutoGen vs AG2 divergence at the time of writing was not investigated.
- Coverage thresholds in Caddy/LangChain: coverage is collected/uploaded; an enforcing threshold
  was not confirmed from the workflow files read.
