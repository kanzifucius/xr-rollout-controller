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

One Secret per cluster, in the release namespace. The [install](../install.md) example uses `xr-rollout-controller-system`. The Secret name is the cluster name. The annotation `rollouts.platform.example.org/environment` is the promotion key, so `acc` can be `acceptance`. Local mode does not use this Secret. Remote and hub both do. The chart does not create it.

## Each spoke

Install [local](local.md). Put the `CompositionRollout` on the spoke.

Bind the kubeconfig user `xr-rollout-promoter` to the ClusterRole in `config/rbac/promoter_remote_role.yaml` (the chart copies it to `charts/xr-rollout-controller/files/promoter-remote-rbac.yaml`). The role is `xr-rollout-promoter`. It can get and patch `CompositionRollout`, which is how `Start` is appended, and it can read CompositionRevisions. It cannot write status and it cannot patch XRs. Do not give that user the XR aggregate role. The spoke controller's ServiceAccount keeps that.

## Example

```yaml
mode: remote
promoter:
  clusters:
    - dev
    - acc
```

Apply this ExternalSecret, or apply the Secret it writes. `vault` is an example SecretStore. The template sets the label and the annotation on the Secret.

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: acc
  namespace: xr-rollout-controller-system
spec:
  secretStoreRef:
    name: vault
    kind: SecretStore
  target:
    name: acc
    template:
      metadata:
        labels:
          sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
        annotations:
          rollouts.platform.example.org/environment: acceptance
  data:
    - secretKey: kubeconfig
      remoteRef:
        key: clusters/acc
        property: kubeconfig
```

The user in the kubeconfig is `xr-rollout-promoter`. The client certificate and key are that user's credentials.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: acc
  namespace: xr-rollout-controller-system
  labels:
    sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
  annotations:
    rollouts.platform.example.org/environment: acceptance
type: Opaque
stringData:
  kubeconfig: |
    apiVersion: v1
    kind: Config
    clusters:
      - name: acc
        cluster:
          server: https://acc.example.org:6443
          certificate-authority-data: <certificate-authority-data>
    contexts:
      - name: acc
        context:
          cluster: acc
          user: xr-rollout-promoter
    current-context: acc
    users:
      - name: xr-rollout-promoter
        user:
          client-certificate-data: <client-certificate-data>
          client-key-data: <client-key-data>
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
