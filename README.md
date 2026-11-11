# xr-rollout-controller

[![Tests](https://github.com/kanzifucius/xr-rollout-controller/actions/workflows/test.yml/badge.svg)](https://github.com/kanzifucius/xr-rollout-controller/actions/workflows/test.yml)
[![Lint](https://github.com/kanzifucius/xr-rollout-controller/actions/workflows/lint.yml/badge.svg)](https://github.com/kanzifucius/xr-rollout-controller/actions/workflows/lint.yml)
[![Docs](https://img.shields.io/badge/docs-GitHub%20Pages-blue)](https://kanzifucius.github.io/xr-rollout-controller/)
[![GitHub release](https://img.shields.io/github/v/release/kanzifucius/xr-rollout-controller?sort=semver)](https://github.com/kanzifucius/xr-rollout-controller/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/kanzifucius/xr-rollout-controller)](https://github.com/kanzifucius/xr-rollout-controller/blob/main/go.mod)
[![Licence](https://img.shields.io/badge/licence-Apache%202.0-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)

Kubebuilder controller that safely rolls out Crossplane **CompositionRevision** upgrades to composite resources (XRs) with canaries, soak-based health checks, per-environment promotion gates, and batched waves.

**Target:** Crossplane v2 only (no claims; XRs may be namespaced). Governs XRs with `compositionUpdatePolicy: Manual` by writing `spec.crossplane.compositionRevisionRef`.

**Documentation:** [https://kanzifucius.github.io/xr-rollout-controller/](https://kanzifucius.github.io/xr-rollout-controller/) (Material for MkDocs + mike; the docs workflow publishes the `gh-pages` tree to GitHub Pages).

Design: [docs/xr-rollout-controller-plan.md](docs/xr-rollout-controller-plan.md)  
Implementation phases: [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md)

## Quick start

```bash
# requires Go 1.23+
make generate manifests
make test   # unit tests (envtest skipped if binaries absent)
make docker-build IMG=xr-rollout-controller:dev
make deploy IMG=xr-rollout-controller:dev
```

Controller flags (set on the Deployment):

| Flag | Default | Purpose |
|------|---------|---------|
| `--environment` | `dev` | Selects `spec.promotion.environments[name]` (or defaultPolicy) |
| `--force-ownership` | `false` | Force SSA field ownership on XR revision refs |
| `--metrics-aggregate-only` | `false` | Drop per-composition Prometheus labels |

## Install

Releases publish a multi-arch controller image and a Helm chart to GHCR. The image and chart are public, so pull and install them without a registry login or an image pull secret.

```bash
helm install xr-rollout-controller \
  oci://ghcr.io/kanzifucius/charts/xr-rollout-controller \
  --version 1.2.3 \
  --namespace xr-rollout-controller-system \
  --create-namespace \
  --set environment=dev
```

Replace `1.2.3` with the chart version. That is the git tag with the leading `v` removed. The chart installs the CompositionRollout CRD, the controller Deployment, RBAC, a metrics Service, and the example ValidatingAdmissionPolicy.

| Value | Default | Purpose |
|-------|---------|---------|
| `image.repository` | `ghcr.io/kanzifucius/xr-rollout-controller` | Controller image repository |
| `image.tag` | chart `appVersion` | Image tag. Release charts set `appVersion` to the git tag, for example `v1.2.3` |
| `environment` | `dev` | Passed as `--environment` |
| `resources` | 10m/64Mi request, 500m/128Mi limit | Container resources |
| `metrics.service.enabled` | `true` | Metrics Service on port 8443 |
| `metrics.serviceMonitor.enabled` | `false` | Prometheus Operator ServiceMonitor. Requires the metrics Service |
| `admissionPolicy.enabled` | `true` | Example ValidatingAdmissionPolicy |

The image reference inside a published chart is `ghcr.io/kanzifucius/xr-rollout-controller:vX.Y.Z`.

```bash
helm upgrade --install xr-rollout-controller \
  oci://ghcr.io/kanzifucius/charts/xr-rollout-controller \
  --version 1.2.3 \
  --namespace xr-rollout-controller-system \
  --set environment=prod
```

Approve and Resume triggers are rejected unless the caller is in the group `rollouts.platform.example.org:release-managers`. Set `admissionPolicy.enabled=false` to skip that policy.

XR access is aggregated from ClusterRoles labelled `rollouts.platform.example.org/aggregate-to-rollout-controller=true`. The chart binds that aggregate ClusterRole to the controller ServiceAccount. `aggregationRule` is a top-level field on the ClusterRole.

Helm installs CRDs from `crds/` on the first install and does not upgrade them. After a chart upgrade, apply the `compositionrollouts.crd.yaml` asset from the GitHub Release.

### Container image

Images are built for `linux/amd64` and `linux/arm64` from the repository Dockerfile.

| Tag | Published when |
|-----|----------------|
| `vX.Y.Z` | A semver tag `vX.Y.Z` is pushed |
| `<short-sha>` | A commit is pushed to `main` (7-character SHA) |
| `latest` | A commit is pushed to `main` |

```bash
docker pull ghcr.io/kanzifucius/xr-rollout-controller:latest
```

### Release manifests

Each GitHub Release also attaches `install.yaml` (from `make build-installer`) and `compositionrollouts.crd.yaml`. `install.yaml` includes the CRDs, RBAC, and the controller Deployment pinned to the release image. It does not set `--environment` (the binary default is `dev`) and it does not include the ValidatingAdmissionPolicy. Use the Helm chart to set the environment and install the policy.

`install.yaml` is built from `config/`. In that tree, `aggregationRule` on the XR ClusterRole is nested under `metadata`, which the API server rejects. The Helm chart places `aggregationRule` at the top level of the ClusterRole. Prefer the chart until that manifest is corrected.

Release assets are public with the repository. Download one and apply it:

```bash
gh release download v1.2.3 --repo kanzifucius/xr-rollout-controller --pattern install.yaml
kubectl apply -f install.yaml
```

## CRD

Cluster-scoped `CompositionRollout` (`xrollout`). Sample: `config/samples/rollouts_v1alpha1_compositionrollout.yaml`.

Manual control is via `spec.triggers` (`Start` / `Approve` / `Resume` / `Abort`), revision-bound and acknowledged in status.

## RBAC

- Manager role: CompositionRollouts, Composition(Revision)s (read), Events.
- XR access: aggregate ClusterRole — label per-group roles with `rollouts.platform.example.org/aggregate-to-rollout-controller=true` (see `config/rbac/xr_aggregate_role.yaml`).
- Example admission policy: `config/policy/approval-vap.yaml`.

## Development layout

```
api/v1alpha1/          CompositionRollout CRD types
internal/controller/   reconciler, watches, indexes
internal/rollout/      pure state machine, canary, health, waves, triggers
internal/xp/           unstructured Crossplane XR helpers
internal/metrics/      Prometheus metrics
charts/                Helm chart
config/                CRD, RBAC, samples, admission policy
docs/                  published site (MkDocs) plus design notes
AGENTS.md              guidelines for coding agents
```

## Support and contributing

- [Contributing](CONTRIBUTING.md) — build, test (`make test`), lint, run, and pull requests.
- [Code of conduct](CODE_OF_CONDUCT.md)
- [Security](SECURITY.md) — report a vulnerability privately.
- [Bug and feature issues](https://github.com/kanzifucius/xr-rollout-controller/issues/new/choose)
- Documentation: [MkDocs site](https://kanzifucius.github.io/xr-rollout-controller/). Agent guidelines: [AGENTS.md](AGENTS.md).

## Licence

[Apache-2.0](LICENSE). Copyright 2026 kanzifucius.
