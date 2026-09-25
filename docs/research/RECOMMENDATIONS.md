# Recommendations (decision-ready)

Companion to `docs/research/PRIOR-ART.md`. Scope: keep `j-harness` minimal (one runtime
dependency, pure-Go, stdlib + GitHub Actions). Every item below is either `none` or `CI-only`;
none adds a runtime dependency.

Constraint check against `AGENTS.md`, `tasks/roadmap.md`, and `docs/MEMORY.md`:

- "No new dependencies without a decision entry." All lint/eval tools below run in CI or
  developer machines, never in the shipped binary.
- "The registry is the deployment." Docs/API changes must stay in lockstep; R2 and R6 directly
  protect this.
- Current CI already has lint (gofmt/vet/staticcheck), test (matrix, `-race`), vuln
  (govulncheck), and smoke. Nothing below replaces those; it fills gaps.

## Recommendation table

| ID | Recommendation | Category | Effort | Impact | Dependency added |
|---|---|---|---|---|---|
| R1 | Real HTTP E2E test with a stdlib `httptest` OpenAI-compatible stub, driving the full async API | E2E tests | S | High | none |
| R2 | Docs freshness gate: `lychee` (offline + fragments) + `markdownlint-cli2` + a Go drift test for routes/env vars | Docs sync | M | High | CI-only (linters), none (Go test) |
| R3 | Checked-in eval suite: deterministic `go test` runner first, nightly Ollama run second, optional promptfoo comment | LLM evals | M | High | none (runner); CI-only (promptfoo, optional) |
| R4 | Pin CI tool versions; add coverage gate via `go test -coverprofile` + Makefile threshold | Quality gates | S | Medium | none |
| R5 | PR hygiene + branch-protection safety: Conventional Commits title check, PR template with docs checklist, one always-green required job | Quality gates | S | Medium | CI-only |
| R6 | Golden-file tests for public wire format (`GET /v1/sessions/{id}` and steps) | E2E tests | S | Medium | none |
| R7 | Scheduled/manual docker-compose E2E: harness + ollama, submit checked-in agent, poll to done | E2E tests | M | Medium | none |
| R8 | Generated, versioned docs site from `docs/*.md` | Docs sync | M | Low now | CI-only |

Dependency note: `staticcheck`, `govulncheck`, `lychee`, `markdownlint-cli2`, and promptfoo are
developer/CI tools only. They must not be added to `go.mod` or the Docker image.

## Rationale per item

### R1 - real HTTP E2E with a stub provider (S, High)

Today the engine is tested with `llm.Fake` (in-process, deterministic) and `scripts/smoke.sh`
boots the binary but never calls a model. Neither proves the README's central claim: an
OpenAI-compatible endpoint works end to end. Add a test that starts `httptest.NewServer`
returning a canned `/v1/chat/completions` response, points the harness `--base-url`/config at
it, submits an agent, and polls `GET /v1/sessions/{id}` until `COMPLETED`. It exercises the real
`internal/llm/openai.go` client, retry decorator, engine, worker, and store with no network,
cost, or new dependency. This is the single highest-value gap to close.

### R2 - docs freshness gate (M, High)

Two layers:

1. Link/prose lint (CI-only): `lycheeverse/lychee-action` with `--offline --include-fragments`
   over `README.md` and `docs/**/*.md` on PRs (Pydantic AI's split: offline on PR, weekly
   external sweep filing an issue). Add `markdownlint-cli2` with a minimal config that matches
   the existing docs (headings, fenced code, no trailing whitespace). Avoid Vale: it is a heavier
   prose linter and the repo has no style guide yet.
2. Routes/env drift test (pure Go, no dependency): define the HTTP route table and the env-var
   list as code, then add a test that parses `docs/API.md` and asserts every documented route,
   status code, and env var is present in code and vice versa. This is the mechanism that
   actually prevents drift for a single-binary project; the linters only catch broken links.

### R3 - eval suite (M, High)

Shape it after the strongest patterns found: Inspect's determinism-first discipline
(`mockllm` E2E, explicit scoring, versioned tasks) plus DSPy's scheduled real-model job.

- `evals/` contains a JSONL dataset (inputs) and cases (expected label, JSON-schema, tool-call
  shape).
- A small Go test runner (`go test ./evals/...`) scores against a scripted fake client - runs on
  every PR, zero cost, deterministic.
- A nightly workflow (DSPy pattern) runs the same cases against Ollama from the existing
  `docker-compose.yml`, caches the model, warms it with retries, and does not block PRs.
- Optional: promptfoo as a CI-only before/after PR comment if richer assertions or model-graded
  scoring are wanted later. Treat it as additive, not the source of truth.

Do not build a full eval framework. The value is a fixed, versioned set of regression cases with
a known baseline.

### R4 - pinned tools and coverage gate (S, Medium)

Replace `go install ...@latest` for `staticcheck` and `govulncheck` with pinned versions so CI
is reproducible and a tool release cannot break the build. Add `-coverprofile` to the test
target and a Makefile check that fails under a modest threshold (start low, e.g. 60 percent, and
raise as coverage grows). Enforce in-repo; do not add Codecov as the only gate. This mirrors
promptfoo's in-repo coverage ratchet.

### R5 - PR hygiene and branch-protection safety (S, Medium)

Add `amannn/action-semantic-pull-request` for Conventional Commits titles (LangChain, DeepEval,
DSPy all do this), a `CODEOWNERS`, and a PR template with a docs checklist. Add one
stable-named, always-passing job for docs-only PRs so path filters cannot deadlock required
checks (Haystack and Prometheus both do this deliberately). Low cost, removes a class of
process friction.

### R6 - golden files for the wire format (S, Medium)

The session JSON is the public contract the README documents. Store canonical responses for
completed, failed, and pipeline-with-skipped-branches cases and diff against them. API changes
then require an intentional golden update, which is a good review signal and cheaper than a
schema-registry dependency.

### R7 - compose E2E, scheduled or manual (M, Medium)

Reuse Phase 10's `docker-compose.yml` (harness + ollama). Boot it, submit a checked-in agent,
poll to completion, tear down. Run nightly or via `workflow_dispatch`, never as a required
check. This is the only test that exercises the shipped container path and the real model
backend. Keep it out of the critical path to control cost and flakiness.

### R8 - generated, versioned docs site (M, Low now)

The hand-written `index.html` is fine at v0.1.0. Only when the public API stabilizes should the
repo generate a site from `docs/*.md` (MkDocs Material or Astro Starlight) and version it per
release. Generating API reference before the surface is stable adds churn; R2's drift test gives
most of the safety at a fraction of the cost. Defer until the roadmap explicitly opens a docs
phase.

## Proposed ordered plan

Each step is independently shippable and verifiable. Run `make fmt-check vet test build` plus
`scripts/smoke.sh` before each.

1. R1 - stub-provider E2E test. Highest value, no dependency, no CI changes.
2. R6 - golden files for the wire format. Small, builds on R1's fixtures.
3. R2 - link/markdown lint (CI-only) plus the Go routes/env drift test.
4. R4 - pin tool versions and add the coverage gate.
5. R3 - deterministic eval runner on PRs; nightly Ollama job next.
6. R5 - PR title check, PR template, always-green docs-only job.
7. R7 - scheduled compose E2E.
8. R8 - only when a docs phase is opened; otherwise leave deferred.

### Acceptance criteria for the whole set

- `make fmt-check vet test build` and `scripts/smoke.sh` pass.
- CI gains: docs lint, routes/env drift, coverage threshold, R1 E2E, and a stable-named
  docs-only job; tool versions pinned.
- No new entry in the `require` block of `go.mod`; no new binary dependency in the Docker image.
- `README.md` and `docs/API.md` updated in the same change as any public API change, enforced by
  the R2 test.
- `tasks/todo.md` records what changed and how it was verified; `docs/MEMORY.md` gets a line for
  any architectural decision (e.g. the eval dataset format).
