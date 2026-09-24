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

### 2026-09-24 — `.nojekyll` breaks README-only GitHub Pages
- **Failure mode:** Published the repo root to the `gh-pages` branch with a committed
  `.nojekyll` marker. Jekyll was disabled, so `README.md` was never converted to `index.html`;
  the Pages site returned HTTP 404 even though the workflow went green ("built").
- **Detection signal:** `gh api .../pages` reported `status: built` but `curl` on the Pages URL
  returned 404; cloning the `gh-pages` branch showed no `index.html` and a committed `.nojekyll`.
- **Prevention rule:** When publishing a Markdown-only site, do **not** ship `.nojekyll` and do
  provide a `_config.yml` (e.g. `theme: jekyll-theme-cayman`) so README renders to `index.html`.
  Verify the rendered URL returns 200, not just that the workflow succeeded.
