# Lessons

Failure modes discovered while building this repo, with detection signal + prevention rule.
Append a new entry after any correction or postmortem. Newest first.

## Format

```
### YYYY-MM-DD — <short title>
- **Failure mode:** what went wrong
- **Detection signal:** how we noticed
- **Prevention rule:** what to do instead
```

_No entries yet._

### 2026-09-24 — Three concurrency-loop bugs in the DAG scheduler
- **Failure mode:** The first `RunPipeline` DAG scheduler (a) exited as soon as no steps were
  pending, while goroutines were still in flight, losing a branch's output; (b) re-admitted
  branch-loser steps that had been marked `SKIPPED`, so they ran anyway; and (c) kept admitting
  new steps after a failure because only `running` was checked.
- **Detection signal:** `TestRunPipelineDAGFanOutFanIn` returned a truncated output; branch
  tests showed skipped steps executing; failure tests ran extra steps.
- **Prevention rule:** A concurrency scheduler must drain in-flight work before exiting
  (`pending == 0 && running == 0`), must remove terminal steps from the pending set (not just
  mark state), and must gate new admissions on the first failure. Prefer a single owner of the
  pending/state maps with worker results delivered on a channel.

### 2026-09-24 — Flaky `TestSubmitQueueFull` (worker dequeue race)
- **Failure mode:** The test submitted three jobs to a pool with `Workers:1, Queue:1` and
  expected the third to hit `ErrQueueFull`. On faster runners (macOS CI) the worker had
  already dequeued job 1 before job 2 was submitted, so job 2 landed in the queue and job 3
  succeeded instead of being rejected, failing the assertion.
- **Detection signal:** `test (macos-latest, 1.27)` failed with `submit b2: engine: job queue
  is full`; the same test passed on Linux.
- **Prevention rule:** Never rely on goroutine scheduling for determinism. Make the worker's
  progress observable (a `started` channel closed inside `Complete`) and block the test until
  the worker is provably busy before asserting queue-full behavior.

### 2026-09-24 — Registry schemas must live inside the registry bundle
- **Failure mode:** Blueprints reference `output_schema` by a path relative to the registry
  root (`schemas/triage.json`), but `schemas/` lived at the repo top level. A registry
  fetched by `install.sh` was therefore not self-contained, and `uninstall.sh` left an
  orphaned `~/.config/j-harness/schemas` directory behind.
- **Detection signal:** Ran the real one-liner installer, then the uninstaller, and found
  `~/.config/j-harness/schemas` still present.
- **Prevention rule:** Anything a blueprint references by a registry-relative path must be
  shipped inside `agent-registry/`. Treat the registry as one self-contained bundle for
  install / run / Docker / uninstall.

### 2026-09-24 — `.nojekyll` breaks README-only GitHub Pages
- **Failure mode:** Published the repo root to the `gh-pages` branch with a committed
  `.nojekyll` marker. Jekyll was disabled, so `README.md` was never converted to `index.html`;
  the Pages site returned HTTP 404 even though the workflow went green ("built").
- **Detection signal:** `gh api .../pages` reported `status: built` but `curl` on the Pages URL
  returned 404; cloning the `gh-pages` branch showed no `index.html` and a committed `.nojekyll`.
- **Prevention rule:** When publishing a Markdown-only site, do **not** ship `.nojekyll` and do
  provide a `_config.yml` (e.g. `theme: jekyll-theme-cayman`) so README renders to `index.html`.
  Verify the rendered URL returns 200, not just that the workflow succeeded.
