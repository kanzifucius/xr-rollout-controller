# Remote

Remote and hub use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. It is not a GA multi-cluster API. Engagement, kubeconfig Secrets, and the hub layout can change. [Local](local.md) mode does not use it.

Pick remote when each cluster should roll its own composite resources (XRs), and one management install should open the next cluster by appending `Start`. The management process does not patch an XR.

Each spoke runs [local](local.md) mode. `Start` is written on the spoke `CompositionRollout`.

## What you get

A spoke keeps rolling if you take the management kubeconfig away. You lose the next `Start`, and that is all this install was doing. Health stays on the spoke object.

You install and upgrade a controller on every spoke, and the `CompositionRollout` CRD has to be there too. The management install depends on the alpha library above.

## Management cluster

Set `mode: remote`. The chart installs the controller, the `PromotionPipeline` CRD, and a Role that can read the kubeconfig Secrets. It does not run the local rollout controller, and it does not bind the XR aggregate role to this ServiceAccount.

The ServiceAccount can update `PromotionPipeline`, including status.

One Secret per cluster. The name is the cluster name. The annotation is the environment key. A Secret named `acc` can carry environment `acceptance`.

## Each spoke

Install [local](local.md). Put the `CompositionRollout` on the spoke.

Bind the kubeconfig user to the ClusterRole in `config/rbac/promoter_remote_role.yaml` (the chart copies it to `charts/xr-rollout-controller/files/promoter-remote-rbac.yaml`). The role is `xr-rollout-promoter`. It can get and patch `CompositionRollout`, which is how `Start` is appended, and it can read CompositionRevisions. It cannot write status and it cannot patch XRs. Do not give that user the XR aggregate role. The spoke controller's ServiceAccount keeps that.

## Example

```yaml
mode: remote
promoter:
  clusters:
    - dev
    - acc
```

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: acc
  namespace: xr-rollout
  labels:
    sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
  annotations:
    rollouts.platform.example.org/environment: acceptance
type: Opaque
data:
  kubeconfig: <base64 kubeconfig>
```

Apply the same object name on each spoke. `dev` can keep `autoStart: true`. Later clusters set `autoStart: false` so they wait for `Start`. Use server-side apply so Git does not own that trigger key. The field manager is `xr-rollout-promoter`.

```yaml
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-payments
spec:
  compositions:
    - name: platformdb.example.org
  autoStart: false
  canary:
    strategy: Percent
    percent: 5
  soak:
    duration: 1h
  promotion:
    defaultPolicy: ManualApproval
    environments:
      acceptance: ManualApproval
  waves:
    batchSize: "10%"
```

Hop behaviour is in [Promotion across clusters](../promotion.md).
