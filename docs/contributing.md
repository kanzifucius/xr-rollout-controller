# Contributing

Source and pull requests live in the GitHub repository:

**[https://github.com/kanzifucius/xr-rollout-controller](https://github.com/kanzifucius/xr-rollout-controller)**

## Local checks

```bash
make test
make docker-build IMG=xr-rollout-controller:dev
```

`make test` runs unit tests. The envtest suite skips cleanly when the Kubebuilder binaries are absent.

## Documentation site

```bash
python3 -m pip install -r requirements-docs.txt
mkdocs build --strict
mkdocs serve   # http://127.0.0.1:8000
```

Site layout (Material tabs): **Home → Install → CompositionRollout → Architecture → Releases → Contributing**.

- Theme tokens and Archify iframe styles: `docs/stylesheets/extra.css`
- Architecture diagrams: standalone HTML under `docs/architecture/` plus caption pages that embed them
- Versioned deploys use [mike](https://github.com/jimporter/mike) on the `gh-pages` branch (see `.github/workflows/docs.yml`)

Prefer editing Markdown under `docs/` and `mkdocs.yml` on a feature branch. CI builds the site on pull requests that touch the docs toolchain, and publishes a version on pushes to `main` and on `v*` tags.

Design notes that are not part of the published site:

- [`docs/xr-rollout-controller-plan.md`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/docs/xr-rollout-controller-plan.md)
- [`docs/IMPLEMENTATION.md`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/docs/IMPLEMENTATION.md)

Agent conventions for this repository are in [`AGENTS.md`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/AGENTS.md).

## Conventions

- British English in user-facing docs.
- Document the public GHCR image and OCI chart. Do not invent a custom docs domain.
- Regenerate Archify HTML when the runtime, rollout flow, or trigger sequence changes. Candidates live under `.archify/` and are not committed. Pin evidence with `--repo-root` and `--quality showcase`.
