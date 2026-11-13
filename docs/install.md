# Install

Releases publish a multi-arch controller image and a Helm chart to GHCR. Both packages are public: pull and install them without a registry login or an image pull secret.

Chart `--version` is SemVer **without** a leading `v`. Image tags **keep** `v`. A pre-release chart keeps the suffix, for example `1.2.3-rc.1`.

```bash
helm install xr-rollout-controller \
  oci://ghcr.io/kanzifucius/charts/xr-rollout-controller \
  --version 1.2.3 \
  --namespace xr-rollout-controller-system \
  --create-namespace \
  --set environment=dev
```

Replace `1.2.3` with the chart version (the git tag with the leading `v` removed). The chart installs the CompositionRollout CRD, the controller Deployment, RBAC, a metrics Service, and the example ValidatingAdmissionPolicy.

## Image

Images are built for `linux/amd64` and `linux/arm64`.

| Tag | Published when |
|-----|----------------|
| `vX.Y.Z` | A stable semver tag is pushed, including optional `+build` metadata. The image is that tag only. |
| `vX.Y.Z-<pre>` | A pre-release tag such as `v1.2.3-rc.1` or `v1.2.3-alpha.1` is pushed. The image is that tag only. `latest` is left unchanged. |
| `<short-sha>` | A commit is pushed to `main` (7-character SHA) |
| `latest` | A commit is pushed to `main` |

```bash
docker pull ghcr.io/kanzifucius/xr-rollout-controller:latest
```

The image reference inside a published chart is `ghcr.io/kanzifucius/xr-rollout-controller:vX.Y.Z`.

Private forks still need a token with `read:packages` and an image pull secret. Pass it with `--set imagePullSecrets[0].name=ghcr`.

## Chart values

| Value | Default | Purpose |
|-------|---------|---------|
| `image.repository` | `ghcr.io/kanzifucius/xr-rollout-controller` | Controller image repository |
| `image.tag` | chart `appVersion` | Image tag. Release charts set `appVersion` to the git tag, for example `v1.2.3` |
| `environment` | `dev` | Passed as `--environment` |
| `remoteReconcile.enabled` | `false` | Reconcile engaged clusters from this install. Requires `rollout.enabled=false`. See [Remote reconcile](remote-reconcile.md). |
| `forceOwnership` | `false` | Force server-side apply ownership of the revision ref |
| `metricsAggregateOnly` | `false` | Drop per-composition Prometheus labels |
| `resources` | 10m/64Mi request, 500m/128Mi limit | Container resources |
| `metrics.service.enabled` | `true` | Metrics Service on port 8443 |
| `metrics.serviceMonitor.enabled` | `false` | Prometheus Operator ServiceMonitor |
| `admissionPolicy.enabled` | `true` | Example ValidatingAdmissionPolicy |

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

## Release manifests

Each GitHub Release also attaches `install.yaml` and `compositionrollouts.crd.yaml`. `install.yaml` includes the CRDs, RBAC, and the controller Deployment pinned to the release image. It does not set `--environment` (the binary default is `dev`) and it does not include the ValidatingAdmissionPolicy. Use the Helm chart when you need the environment flag and the policy.

`install.yaml` is built from `config/`. In that tree, `aggregationRule` on the XR ClusterRole is nested under `metadata`, which the API server rejects. The Helm chart places `aggregationRule` at the top level. Prefer the chart until that manifest is corrected.

```bash
gh release download v1.2.3 --repo kanzifucius/xr-rollout-controller --pattern install.yaml
kubectl apply -f install.yaml
```

Cutting a tag publishes the image, the chart, the GitHub Release, and a docs version together. See [Releases](releases.md).
