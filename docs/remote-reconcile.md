# Remote reconcile

`--remote-reconcile` runs the CompositionRollout controller in the management cluster, once, for every engaged spoke. Canary selection and the `compositionRevisionRef` patch use that spoke's API. Spoke clusters do not run a controller pod.

The flag defaults to off. Off, the controller stays on the cluster it is installed in and still reads `--environment`.

Turn it on from the chart with the local controller off:

```bash
helm upgrade --install xr-rollout-controller charts/xr-rollout-controller \
  --set rollout.enabled=false \
  --set remoteReconcile.enabled=true \
  --set promoter.enabled=true
```

`promoter.enabled` is optional. The promoter still only appends `Start`. It does not patch composite resources (XRs).

## Environment

Each kubeconfig Secret carries that cluster's environment. The Secret name is the cluster name. The annotation is the promotion key. They are different strings:

```yaml
metadata:
  name: acc
  labels:
    sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
  annotations:
    rollouts.platform.example.org/environment: acceptance
```

The process `--environment` is not used for these clusters. A missing annotation stops that cluster's reconcile: no XR patch and no status write, until the annotation is set. If the annotation is set and `spec.promotion.environments` has no entry for it, `defaultPolicy` applies, which defaults to `ManualApproval`.

`--force-ownership` and `--metrics-aggregate-only` still apply to every cluster.

## What stays on the spoke

Apply the `CompositionRollout` CRD, your `CompositionRollout` objects, and Crossplane. Apply `config/policy/approval-vap.yaml` if `Approve` and `Resume` should be limited to release managers. The policy has to be on the spoke API server.

Apply `config/rbac/remote_reconcile_role.yaml` and bind it to the user in the kubeconfig. Also label one ClusterRole per XR API group with `rollouts.platform.example.org/aggregate-to-rollout-controller: "true"`. The aggregate role in that file binds those groups to the same user. `aggregationRule` stays a top-level field.

Do not install the controller Deployment, its ServiceAccount, or a leader-election lease on the spoke. Do not set a pod `--environment` there. Do not install the `PromotionPipeline` CRD on the spoke.

Do not leave the old spoke controller running. It would write the same XR field as this process, both as `xr-rollout-controller`.

## Dry run and rollback

`spec.dryRun: true` still does not patch XRs, does not write `compositionUpdatePolicy`, and does not publish phase `Completed`. Abort and pause still do not roll XRs back.
