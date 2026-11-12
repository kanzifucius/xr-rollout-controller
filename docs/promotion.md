# Promotion across clusters

The promoter runs in the management cluster. Each spoke still runs its own rollout controller, and that controller still rolls only that cluster's composite resources (XRs). The promoter does not patch an XR. It opens the next cluster's gate by appending one `Start` trigger.

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

The order is not the order the Secrets were created. It is `PromotionPipeline.spec.clusters`, which the chart renders from `promoter.clusters` (default `dev`, `test`, `acc`, `prod`). Enable the promoter with `--set promoter.enabled=true` and, on that management install, `--set rollout.enabled=false`. A spoke install does the opposite: the rollout controller stays on, and the promoter stays off.

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

On each spoke, bind the kubeconfig identity to the ClusterRole in `config/rbac/promoter_remote_role.yaml` (also packaged at `charts/xr-rollout-controller/files/promoter-remote-rbac.yaml`). Do not reuse the XR aggregation role. That remote role can read rollout status and patch `spec.triggers`. It cannot write status, and it cannot touch XRs.

The management process reaches each cluster with the [multicluster-runtime](https://github.com/kubernetes/community/blob/main/sig-multicluster/README.md) client library. Nothing else from that SIG has to be installed on the spokes.
