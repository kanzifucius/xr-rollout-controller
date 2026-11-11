# Agent Guidelines for xr-rollout-controller

Guidelines for coding agents working in this repository.
The controller rolls Crossplane v2 CompositionRevision upgrades onto Manual composite resources (XRs): canary, soak, per-environment gate, then waves. It does not roll back.

## Project Overview

- **Language**: Go 1.23+ (`go.mod`)
- **Scaffold**: Kubebuilder / controller-runtime
- **Module path**: `github.com/kanzifucius/xr-rollout-controller`
- **API**: cluster-scoped `CompositionRollout` (`rollouts.platform.example.org/v1alpha1`, short name `xrollout`)
- **Crossplane**: v2 only. No claims. XRs may be namespaced or cluster-scoped.
- **Write surface**: server-side apply of `spec.crossplane.compositionRevisionRef.name` on Manual XRs only. Field manager `xr-rollout-controller`. Never write `compositionUpdatePolicy`, `compositionRef`, labels, or XR schema fields.

User docs: [https://kanzifucius.github.io/xr-rollout-controller/](https://kanzifucius.github.io/xr-rollout-controller/). Design notes that are not on that site: `docs/xr-rollout-controller-plan.md`, `docs/IMPLEMENTATION.md`.

## Build and test

```bash
make generate manifests   # controller-gen (deepcopy, CRDs, RBAC)
make test                 # unit tests; envtest skips when Kubebuilder assets are absent
make lint                 # golangci-lint (version pinned in the Makefile)
make docker-build IMG=xr-rollout-controller:dev

# One package or one test
go test ./internal/rollout/ -run TestTransitionSoakPassedPolicies -count=1
```

`make test` also runs `generate`, `manifests`, `fmt`, and `vet`. Do not commit `cover.out`, `bin/`, or `site/`.

Kind end-to-end is `make test-e2e` and needs a running kind cluster. It is not the required CI job. CI runs `.github/workflows/test.yml` (`make test`) and `.github/workflows/lint.yml`.

## Code map

```
cmd/main.go                 # flags: --environment, --force-ownership, --metrics-aggregate-only
api/v1alpha1/               # CompositionRollout types and generated deepcopy
internal/controller/        # reconciler, watches, indexes, SSA patch
internal/rollout/           # pure state machine, canary, health, waves, triggers
internal/xp/                # unstructured XR accessors
internal/metrics/           # Prometheus metrics (no per-XR labels)
charts/xr-rollout-controller/  # Helm chart, including CRDs and the admission policy
config/                     # kubebuilder CRD, RBAC, samples, kustomize installer
```

Phase changes belong in `internal/rollout` (`Transition` and the trigger helpers). The reconciler applies that decision: list the fleet from the cache, patch, write status. Keep the state machine free of Kubernetes clients so table tests stay in `internal/rollout`.

Switch on `RolloutPhase`, `TriggerAction`, `PromotionPolicy`, and `CanaryStrategy` exhaustively. The `default` arm must assign the value to a `never` type (see `impossiblePhase` and `impossibleTriggerAction`) so a new variant fails the build.

## Behaviour that must stay true

- Canary membership is frozen in `status.rollouts[].canary.selected`. Do not recompute it mid-rollout.
- `Count` and `Percent` selection is a hash of the XR UID, with namespace and name as the tie-break. `preferredNamespaces` is ignored for `LabelSelector`.
- Soak health is `Ready` and `Synced` both `True`, and stable since the soak started. A mid-window flip to `False` fails that XR.
- `--environment` selects `spec.promotion.environments[name]`. A missing key uses `defaultPolicy`, which defaults to `ManualApproval`.
- `spec.triggers` is append-only from the controller's point of view. Acknowledge in status. Do not delete or edit spec. `Approve`, `Resume`, and `Abort` must match the target revision or they are `Stale`.
- Abort and pause do not revert XRs already moved to the new revision.
- `spec.suspend` pauses every composition and ignores triggers until it is cleared.
- Two `CompositionRollout` objects must not claim the same composition. Admission rejects it; the reconciler still pauses on overlap.
- Metrics must not use per-XR labels. `--metrics-aggregate-only` drops the composition label.

## Helm and release

The chart is the supported install. `aggregationRule` on the XR ClusterRole is a top-level field in the chart. In `config/` it is still nested under `metadata`, which the API server rejects. Do not "fix" the chart by copying that nesting. Prefer the chart over `install.yaml` until that manifest is corrected.

`make build-installer` rewrites `config/manager` image settings. Do not commit that kustomize edit.

`.github/workflows/release.yml` publishes the image, GitHub Release assets, and the OCI chart. Do not fold docs publishing into that file. `.github/workflows/docs.yml` publishes the site on the same tag push.

Image tags keep the leading `v` (`v1.2.3`). Chart versions strip it (`1.2.3`). A pre-release tag (`v1.2.3-rc.1`, `v1.2.3-alpha.1`, optional `+build` metadata) publishes that image tag only, a GitHub pre-release (`--prerelease`, `--latest=false`, generated notes, the same `install.yaml` and CRD assets), and the chart version without `v`. It does not move the image `latest` tag, retag a stable or `latest` chart, or move the mike `latest` alias. Stable `vMAJOR.MINOR.PATCH` tags (optional `+build`) keep the current publish path, including moving the mike `latest` alias. Pushes to `main` still publish the short SHA and image `latest`, and docs version `dev`. The split is `hack/release-tag.sh`, checked by `hack/release-tag_test.sh` (`make test-release-tag`). Packages are public (no pull secret in the default install).

## Docs site

```bash
python3 -m pip install -r requirements-docs.txt
mkdocs build --strict
```

- British English in user-facing docs (`organisation`, `behaviour`, `stabilisation`).
- The site URL is `https://kanzifucius.github.io/xr-rollout-controller/`. Do not add a custom domain.
- mike versions: `dev` from `main`; a stable tag `vX.Y.Z` (optional `+build`) becomes version `X.Y.Z` and moves the `latest` alias. A pre-release tag publishes its own version and leaves `latest` on the last stable version. A hyphen only in build metadata does not make a pre-release. mike commits that tree to `gh-pages`, which Pages serves. Do not put `environment: github-pages` on the docs job: that environment only allows the `gh-pages` branch, so a run on `main` is rejected before mike pushes. Do not call the Pages REST API to create the site: `GITHUB_TOKEN` cannot (HTTP 403).
- `docs/xr-rollout-controller-plan.md` and `docs/IMPLEMENTATION.md` are excluded from the site. Link them from GitHub, not as nav pages.
- Archify HTML under `docs/architecture/` is committed. Candidates under `.archify/` are not. Regenerate with `--repo-root` and `--quality showcase` when the runtime, rollout flow, or Approve sequence changes.

## Style

Imports stay at the top of the file, in gofmt groups: standard library, third-party, then this module. No inline imports.

Wrap errors with context: `fmt.Errorf("index CompositionRollout: %w", err)`. Check the error immediately.

New Go files use the Apache-2.0 header in `hack/boilerplate.go.txt`.

Tests in `internal/rollout` are table-style or direct `Transition` cases, call `t.Parallel()`, and use `t.Fatalf` with the got value. Do not add a Kubernetes client to those tests.

After API field changes, run `make generate manifests` and copy the CRD into `charts/xr-rollout-controller/crds/` so Helm and `config/crd` stay aligned.
