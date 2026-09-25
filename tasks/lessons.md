# Lessons

Failure modes discovered while building this repo, with detection signal + prevention rule.
Append a new entry after any correction or postmortem. Newest first.

## Format

```
### YYYY-MM-DD: <short title>
- **Failure mode:** what went wrong
- **Detection signal:** how we noticed
- **Prevention rule:** what to do instead
```

### 2026-09-25: Do not golden-file `omitempty` fields, and never reach a host service from a container through the host
- **Failure mode:** two independent flakes. (1) `internal/e2e` golden files normalized
  `duration_ms` to `0`, but the field is `omitempty`: sub-millisecond calls omit the key
  entirely while slower ones include it, so the golden failed intermittently under `-race`
  even though the wire contract was unchanged. (2) `scripts/container_e2e.sh` bound the LLM
  stub on the host and pointed the container at `host.docker.internal` via
  `--add-host=...:host-gateway`; the host runs UFW/nftables with `INPUT policy DROP`, so the
  container's request was silently dropped and the job sat in `RUNNING` until the script's
  poll deadline.
- **Detection signal:** (1) `make test` failed in `TestGoldenStepsCompleted` /
  `TestGoldenPipelineSkippedSteps` with a diff whose only delta was the `duration_ms` key,
  and passed on re-run. (2) `docker exec <ctr> wget http://host.docker.internal:<port>/healthz`
  hung, while `iptables -L INPUT` showed policy `DROP`; the same symptom occurred for the
  bridge gateway IP `172.17.0.1`.
- **Prevention rule:** golden files should drop volatile fields entirely rather than
  normalize their values when the field is `omitempty` (presence, not just value, is
  observable). For container E2E, put the dependency service on the *same Docker network*
  and address it by container name over Docker DNS; container-to-container avoids the host
  firewall and `host-gateway` support entirely. A container E2E must also print
  `docker logs` on failure so a stuck `RUNNING` job is diagnosable, not just a timeout.

### 2026-09-25: Per-blueprint tool allowlist must be enforced at call time, not just at resolve time
- **Failure mode:** `resolveTools` checked each blueprint's `tools` list against the global
  enabled registry and advertised only those to the model, but `runTool` looked the called name
  up in the *global* registry alone. A model could therefore invoke any globally-enabled tool
  (e.g. `current_time`) even when the blueprint declared only `math_eval`, silently bypassing
  the per-agent capability boundary.
- **Detection signal:** Code review of the Phase 8 tool loop; the advertised tool list and the
  executable tool set were derived from two different sources.
- **Prevention rule:** When a capability is scoped to an entity (blueprint), enforce that scope
  at execution time too. Resolve the allowlist once into a set, advertise it, and re-check every
  incoming call against the same set before running the tool.

### 2026-09-24: `pkill -f <pattern>` can match the invoking shell
- **Failure mode:** a `pkill -f bin/harness` cleanup command hung, because the pattern also
  matched the shell command line that was running `pkill` itself, which then tried to signal
  the shell that was waiting for it.
- **Detection signal:** the shell tool timed out on a trivial cleanup command; `pgrep -af` then
  showed the matching line was the cleanup command itself.
- **Prevention rule:** kill by PID (`kill $PID`) for processes started by the script, or match a
  pattern that cannot appear in the pkill invocation; verify with `pgrep -af` before and after.

### 2026-09-24: Registry writes must not mutate the live snapshot in place
- **Failure mode:** Editing an agent through the HTTP CRUD API could swap registry fields
  (`Registry` internals) while a worker was mid-run, so an in-flight execution could observe a
  half-updated agent or prompt.
- **Detection signal:** Design review; no test could pin the exact interleaving, but the shared
  pointer was plainly unsynchronized.
- **Prevention rule:** Treat a loaded registry as an immutable snapshot. Serve execution from a
  snapshot pointer; when a write lands, validate + atomically write, reload the *whole* registry,
  then swap the pointer behind a lock (`Engine.SetRegistry` + `RWMutex`). In-flight runs keep the
  snapshot they started with.

### 2026-09-24: Three concurrency-loop bugs in the DAG scheduler
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

### 2026-09-24: Flaky `TestSubmitQueueFull` (worker dequeue race)
- **Failure mode:** The test submitted three jobs to a pool with `Workers:1, Queue:1` and
  expected the third to hit `ErrQueueFull`. On faster runners (macOS CI) the worker had
  already dequeued job 1 before job 2 was submitted, so job 2 landed in the queue and job 3
  succeeded instead of being rejected, failing the assertion.
- **Detection signal:** `test (macos-latest, 1.27)` failed with `submit b2: engine: job queue
  is full`; the same test passed on Linux.
- **Prevention rule:** Never rely on goroutine scheduling for determinism. Make the worker's
  progress observable (a `started` channel closed inside `Complete`) and block the test until
  the worker is provably busy before asserting queue-full behavior.

### 2026-09-24: nil-receiver methods must guard before dereferencing

- **Failure mode:** `(*Metrics).Render` dereferenced the receiver (`for name, c := range m.counters`)
  without a nil check. `Inc`/`Add`/`Get` were safe because they routed through a nil-aware
  `counter(name)` helper, but `Render` did not. `TestNilMetricsSafe` segfaulted.
- **Detection signal:** `go test -race ./...` panicked with a SIGSEGV inside `metrics.go` on a
  package whose tests otherwise passed.
- **Prevention rule:** If a type is designed to be nil-safe (optional dependency), every method
  must guard `if m == nil` before touching fields, and a nil-receiver test must exercise all
  exported methods, not just one.

### 2026-09-24: Registry schemas must live inside the registry bundle
- **Failure mode:** Blueprints reference `output_schema` by a path relative to the registry
  root (`schemas/triage.json`), but `schemas/` lived at the repo top level. A registry
  fetched by `install.sh` was therefore not self-contained, and `uninstall.sh` left an
  orphaned `~/.config/j-harness/schemas` directory behind.
- **Detection signal:** Ran the real one-liner installer, then the uninstaller, and found
  `~/.config/j-harness/schemas` still present.
- **Prevention rule:** Anything a blueprint references by a registry-relative path must be
  shipped inside `agent-registry/`. Treat the registry as one self-contained bundle for
  install / run / Docker / uninstall.

### 2026-09-24: `.nojekyll` breaks README-only GitHub Pages
- **Failure mode:** Published the repo root to the `gh-pages` branch with a committed
  `.nojekyll` marker. Jekyll was disabled, so `README.md` was never converted to `index.html`;
  the Pages site returned HTTP 404 even though the workflow went green ("built").
- **Detection signal:** `gh api .../pages` reported `status: built` but `curl` on the Pages URL
  returned 404; cloning the `gh-pages` branch showed no `index.html` and a committed `.nojekyll`.
- **Prevention rule:** When publishing a Markdown-only site, do **not** ship `.nojekyll` and do
  provide a `_config.yml` (e.g. `theme: jekyll-theme-cayman`) so README renders to `index.html`.
  Verify the rendered URL returns 200, not just that the workflow succeeded.
