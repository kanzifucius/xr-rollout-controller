# Implementation phases

Maps to the design milestones in [xr-rollout-controller-plan.md](xr-rollout-controller-plan.md).

| Phase | Status | What’s in the tree |
|-------|--------|--------------------|
| **M1 — Skeleton & fleet visibility** | Done | Kubebuilder scaffold, `CompositionRollout` CRD, Composition/Revision watches, dynamic XR watch registry, field indexes, status fleet accounting |
| **M2 — Canary engine** | Done | LabelSelector / Count / Percent + `preferredNamespaces`, UID-hash selection, freeze in status, SSA revision-ref patches |
| **M3 — Soak & pause** | Done | Ready/Synced soak evaluation, soak timers via `RequeueAfter`, pause conditions/events, no writes while paused |
| **M4 — Triggers & promotion gate** | Done | `spec.triggers` consumption/ack, `--environment` + promotion map, AwaitingApproval / Resume / Abort |
| **M5 — Waves & scale** | Done (initial) | Batch size (int/%), concurrent patch cap, wave soak, predicates + 30s requeue backstop, metrics |
| **M6 — Production polish** | Partial | Sample admission policy + aggregate RBAC shipped; e2e matrix / runbooks still TODO |

## Parallel work

M1 API + scaffold landed first, then pure packages (`canary`, `health`, `waves`, `statemachine`, `triggers`, `xp`) and the controller wiring were built in parallel streams and merged into this tree.

## Verified locally

```
go build ./...
go test ./internal/rollout/... ./internal/xp/... ./internal/metrics/... ./internal/controller/...
make generate manifests
```

Envtest e2e requires Kubebuilder assets (`make envtest`); the suite skips cleanly when they are absent.
