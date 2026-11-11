# Contributing

The controller rolls Crossplane v2 CompositionRevision upgrades onto composite resources (XRs) whose `compositionUpdatePolicy` is `Manual`.

User documentation is the MkDocs site: [https://kanzifucius.github.io/xr-rollout-controller/](https://kanzifucius.github.io/xr-rollout-controller/). The site's Contributing page ([docs/contributing.md](docs/contributing.md)) covers the docs toolchain. Rules for coding agents, including the behaviour that must stay true, are in [AGENTS.md](AGENTS.md).

## Invariants

A change must not break these. [AGENTS.md](AGENTS.md) lists the rest.

- **Manual revision ref only.** The controller server-side applies `spec.crossplane.compositionRevisionRef` on Manual XRs (field manager `xr-rollout-controller`). It does not write `compositionUpdatePolicy`, `compositionRef`, labels, or XR schema fields. Automatic XRs are counted and left alone.
- **No rollback.** Abort and pause do not revert XRs that have already moved to the new revision.
- **Triggers are acknowledged, not deleted.** `spec.triggers` stays in the spec. The controller records consumption on `status.rollouts[].acknowledgedTriggers` and does not delete or edit the spec entry. `Approve`, `Resume`, and `Abort` must name the target revision, or they are acknowledged as `Stale`.

## Build

Go 1.23 or newer, as in `go.mod`.

```bash
make generate manifests
make build
```

`make build` writes `bin/manager`. Do not commit `bin/`, `cover.out`, or `site/`.

## Test

```bash
make test
```

`make test` runs `generate`, `manifests`, `fmt`, `vet`, then the unit tests. The envtest suite skips when the Kubebuilder assets are absent. CI runs this from `.github/workflows/test.yml`.

One package:

```bash
go test ./internal/rollout/ -count=1
```

`make test-e2e` needs a running kind cluster. It is not the required CI job.

## Lint

```bash
make lint
```

`make lint` installs golangci-lint (version pinned by `GOLANGCI_LINT_VERSION` in the Makefile) and runs `.golangci.yml`. `make lint-fix` applies fixes. CI runs `.github/workflows/lint.yml` with the same version.

## Run

Install the CRDs, then run the controller against the cluster in `~/.kube/config`:

```bash
make install
make run
```

`make run` also runs `generate`, `manifests`, `fmt`, and `vet`.

| Flag | Default | Purpose |
|------|---------|---------|
| `--environment` | `dev` | Selects `spec.promotion.environments[name]` (or `defaultPolicy`) |
| `--force-ownership` | `false` | Force SSA field ownership on XR revision refs |
| `--metrics-aggregate-only` | `false` | Drop per-composition Prometheus labels |

```bash
make docker-build IMG=xr-rollout-controller:dev
make deploy IMG=xr-rollout-controller:dev
```

The Helm chart is the supported install. See the README.

## Documentation site

```bash
python3 -m pip install -r requirements-docs.txt
mkdocs build --strict
```

`mkdocs serve` previews at `http://127.0.0.1:8000`. Layout and versioning notes are in [docs/contributing.md](docs/contributing.md). User-facing docs use British English.

Design notes that are not on the site: [docs/xr-rollout-controller-plan.md](docs/xr-rollout-controller-plan.md), [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md).

## Pull requests

1. Branch from `main`.
2. Run `make test` and `make lint`. After an API field change, also run `make generate manifests` and copy the CRD into `charts/xr-rollout-controller/crds/` so Helm and `config/crd` stay aligned.
3. Open a pull request. The template asks what changed and how you tested it.
4. Tests and Lint must pass.

New Go files use the Apache-2.0 header in `hack/boilerplate.go.txt`. Imports stay at the top of the file.

- Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- Vulnerabilities: [SECURITY.md](SECURITY.md) (a private GitHub advisory, not a public issue)
- Licence: [Apache-2.0](LICENSE)
