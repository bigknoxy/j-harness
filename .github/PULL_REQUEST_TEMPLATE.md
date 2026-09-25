# Pull request

## What

<!-- One paragraph: what changed and why. -->

## Docs

The docs map is [`docs/DOCUMENTATION.md`](../docs/DOCUMENTATION.md); it lists every doc surface and
the code changes that require it to be updated.

- [ ] Docs updated in this PR (README / docs / tasks / index.html as applicable)
- [ ] Or: no docs impact, and that is stated below

Docs impact notes:

<!-- e.g. "no user-visible change" or "updated docs/API.md env table". -->

## Verification

- [ ] `make fmt-check vet test` passes
- [ ] `make docs` passes (routes, env vars, metrics, links, version pin, ASCII)
- [ ] `sh scripts/smoke.sh` passes (if behavior changed)

Commands run / evidence:

## Checklist

- [ ] Commit subject follows Conventional Commits (checked by the `pr-title` job)
- [ ] No new Go module dependency (or a `docs/MEMORY.md` decision entry explains why)
- [ ] No secrets or keys added to code, logs, or this description
