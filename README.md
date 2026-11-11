# xr-rollout-controller

Kubebuilder controller that safely rolls out Crossplane **CompositionRevision** upgrades to composite resources (XRs) with canaries, soak-based health checks, per-environment promotion gates, and batched waves.

**Target:** Crossplane v2 only (no claims; XRs may be namespaced). Governs XRs with `compositionUpdatePolicy: Manual` by writing `spec.crossplane.compositionRevisionRef`.

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
config/                CRD, RBAC, samples, admission policy
docs/                  design + implementation notes
```

## Licence

Apache-2.0
