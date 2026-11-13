# Promotion across clusters

The promoter runs in the management cluster. Each spoke still runs its own rollout controller, and that controller still rolls only that cluster's composite resources (XRs). The promoter does not patch an XR. It opens the next cluster's gate by appending one `Start` trigger.

The management process can also run the rollout controller for those spokes (`--remote-reconcile`). The promoter still only appends `Start`. Spoke Deployments are then uninstalled. The spoke `--environment` pod flag is replaced by the Secret annotation, and `acceptance` is still not required to equal the cluster name `acc`.

One management install is given kubeconfigs for `dev`, `test`, `acc`, and `prod`. Each kubeconfig is a Secret in the release namespace:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dev
  namespace: xr-rollout
  labels:
    sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
type: Opaque
data:
  kubeconfig: <base64 kubeconfig>
```

The Secret name is the cluster name. The data key is `kubeconfig`. Repeat the Secret for `test`, `acc`, and `prod`. The chart does not create these Secrets.

The order is not the order the Secrets were created. It is `PromotionPipeline.spec.clusters`, which the chart renders from `promoter.clusters` (default `dev`, `test`, `acc`, `prod`). A pipeline lists 2 to 32 clusters. Each name is unique and at most 253 characters, the same limit as a Secret name. Enable the promoter with `--set promoter.enabled=true` and, on that management install, `--set rollout.enabled=false`. A spoke install does the opposite: the rollout controller stays on, and the promoter stays off.

`dev` keeps `autoStart: true`. `test`, `acc`, and `prod` set `autoStart: false`. The promoter does not change that field. If a later cluster leaves `autoStart: true`, that cluster can start a revision before the previous one finishes. The promoter will not append a `Start` that the spoke would acknowledge as `Stale`. It records that the revision has already started, and it still waits for that cluster to finish before opening the cluster after it.

A hop writes one trigger:

| Field | Value |
|-------|-------|
| `action` | `Start` |
| `revision` | the completed `CompositionRevision` name |
| `actor` | `xr-rollout-promoter` |

The field manager is `xr-rollout-promoter`. `Paused` or `Aborted` on the lower cluster leaves the next cluster unstarted. Abort does not roll XRs back.

The spoke `--environment` value is still the in-cluster policy key (`acceptance` in the samples). It is not required to equal the cluster name `acc`.

Apply spoke `CompositionRollout` objects with server-side apply, so GitOps does not own the promoter's trigger key. `spec.triggers` is a map keyed by composition, action, and revision. The controller acknowledges a trigger in status. It does not delete it.

A spoke `CompositionRollout` with `spec.dryRun: true` does not receive `Start`. A predecessor that is a dry run does not open the next cluster. The hop reason is `DryRun`. Set `spec.dryRunChain: true` to record that `Start` on the pipeline instead. The trigger is still not written. See [Optional hop rules](#optional-hop-rules).

On each spoke, bind the kubeconfig identity to the ClusterRole in `config/rbac/promoter_remote_role.yaml` (also packaged at `charts/xr-rollout-controller/files/promoter-remote-rbac.yaml`). Do not reuse the XR aggregation role. That remote role can read rollout status and patch `spec.triggers`. It cannot write status, and it cannot touch XRs.

The management process reaches each cluster with the [multicluster-runtime](https://github.com/kubernetes/community/blob/main/sig-multicluster/README.md) client library. Nothing else from that SIG has to be installed on the spokes.

## Optional hop rules

Leave these fields unset and the promoter writes `Start` as soon as the previous cluster has completed that revision. `dev` and `test` stay on that default. `acc` and `prod` can opt in. None of these fields patch an XR, write `compositionUpdatePolicy`, or delete a trigger.

### Approval

Set `approval: Approve` on a cluster. After the previous cluster has completed, the promoter waits until the pipeline contains an `Approve` trigger for that cluster and revision. It then writes `Start`. The trigger stays in `spec.triggers`. The promoter records it on `status.acknowledgedTriggers`.

```yaml
spec:
  clusters:
    - name: dev
    - name: test
    - name: acc
      approval: Approve
    - name: prod
      approval: Approve
  triggers:
    - cluster: acc
      action: Approve
      revision: platformdb.example.org-aaa
      rollout: team-payments
      composition: platformdb.example.org
      actor: release@example.org
```

An empty `rollout` or `composition` matches every rollout or composition on that cluster. This `Approve` is the cross-cluster gate. The in-cluster `Approve` still lives on the spoke `CompositionRollout`.

### Revision match

Set `revisionMatch: true` on a cluster. The hop waits while that cluster's latest `CompositionRevision` for the composition differs from the revision that completed on the previous cluster. The reason is `RevisionMismatch`. The promoter only promotes that same revision. It does not write `Start` for a different one.

Unset, a different latest revision stays `Blocked` with reason `RevisionNotLatest`. That check is unchanged.

### Soak between clusters

Set `soak` on a cluster, for example `1h`. The promoter waits that long after the previous cluster's `completedAt` before it writes `Start`. The reason while it waits is `InterClusterSoak`. This clock is not the in-cluster canary soak (`spec.soak.duration` on the `CompositionRollout`).

### Partial fleets

Set `minCompletedPercent` on a cluster. Matching `CompositionRollout` objects are those on the previous cluster that govern the same composition. `fleetSelector` narrows that set by label. The hop waits until that percentage have completed the revision. The reason is `FleetIncomplete`.

`100` requires every matching rollout. A lower percentage writes `Start` only for the rollouts that have completed the revision, and only once the share is reached. Rollouts of other compositions are not in the percentage. Unset, each `CompositionRollout` is promoted on its own.

### Skip

Append a `Skip` trigger that names a cluster and a revision. The promoter treats that cluster as already satisfied for that revision, so the next cluster can receive `Start` without this cluster having completed. The trigger stays in spec. Skip does not patch XRs and does not roll anything back. A dry-run rollout is not skipped into a real `Start`.

```yaml
spec:
  triggers:
    - cluster: test
      action: Skip
      revision: platformdb.example.org-aaa
```

### Dry-run chain

Set `spec.dryRunChain: true` on the pipeline. When the previous `CompositionRollout` is `spec.dryRun` and the simulation has finished (`status.rollouts[].dryRun.completedAt`), the promoter records the `Start` it would have written on `status.hops[].wouldStart`. It does not append that trigger. The reason is `DryRunRecorded`.

A dry run still does not set `status.rollouts[].completedAt` or phase `Completed`, and it does not patch XRs. `dryRun.completedAt` is not a real completion. Unset, the hop stays `Blocked` with reason `DryRun`. A dry-run destination does not receive `Start` either.
