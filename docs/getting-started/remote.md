# Remote mode

Pick remote when each cluster should roll its own composite resources (XRs), and one management install should open the next cluster by appending `Start`. The management process does not patch an XR. Each spoke runs [local mode](local.md). `Start` is written on the spoke `CompositionRollout`.

Remote and hub use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. It is not a GA multi-cluster API. Kubeconfig Secrets and the hub layout can change. [Local mode](local.md) does not use it.

You install and upgrade a controller on every spoke, and the `CompositionRollout` CRD has to be there too. A spoke keeps rolling if the management kubeconfig is removed. You lose the next `Start`. Health stays on the spoke object.

## Management cluster

Set `mode: remote`. The chart installs the controller, the `PromotionPipeline` CRD, and a Role that can read the kubeconfig Secrets. It does not run the local rollout controller, and it does not bind the XR aggregate role to this ServiceAccount.

The ServiceAccount can update `PromotionPipeline`, including status.

One Secret per cluster, in the release namespace. The [install](../install.md) example uses `xr-rollout-controller-system`. The Secret name is the cluster name (`acc`). On the spoke, Helm `environment` (`--environment`) is the key in `spec.promotion.environments`. Install that spoke with `--set environment=acceptance` when the manifest maps `acceptance` to `ManualApproval`. The annotation `rollouts.platform.example.org/environment` is required in hub mode. Remote mode does not use it for this gate. The chart does not create the Secret. Local mode does not use it.

## Each spoke

Install [local mode](local.md). Put the `CompositionRollout` on the spoke. The Helm command on that page takes this cluster's `--set environment=`.

Bind the kubeconfig user `xr-rollout-promoter` to the ClusterRole in `config/rbac/promoter_remote_role.yaml` (the chart copies it to `charts/xr-rollout-controller/files/promoter-remote-rbac.yaml`). The role is `xr-rollout-promoter`. It can get and patch `CompositionRollout`, which is how `Start` is appended, and it can read CompositionRevisions. It cannot write status and it cannot patch XRs. Do not give that user the XR aggregate role. The spoke controller's ServiceAccount keeps that.

## Example

Helm values:

```yaml
mode: remote
promoter:
  clusters:
    - dev
    - acc
```

```bash
helm upgrade --install xr-rollout-controller \
  oci://ghcr.io/kanzifucius/charts/xr-rollout-controller \
  --version 1.2.3 \
  --namespace xr-rollout-controller-system \
  --create-namespace \
  --set mode=remote \
  --set 'promoter.clusters={dev,acc}'
```

Replace `1.2.3` with the chart version. The spoke install is the [local mode](local.md) command with that cluster's `--set environment=`.

The controller loads this Secret only when it has the label `sigs.k8s.io/multicluster-runtime-kubeconfig: "true"`. The data key is `kubeconfig`.

The user in the kubeconfig needs a static credential: `token`, or `client-certificate-data` and `client-key-data`. The `*-data` values are base64, the same as `kubectl config view --raw`. Do not store a kubeconfig whose user has `exec`. kubelogin, the Azure CLI, and Entra ID are exec plugins. The controller image is distroless and contains only `/manager`, so it cannot run `kubelogin`. An exec kubeconfig still parses, and the first API call fails when client-go tries to run that command. Cache sync fails before the cluster is engaged. A device-code or browser login cannot finish inside the pod.

The user in this kubeconfig is `xr-rollout-promoter`.

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

A bearer token replaces those certificate fields:

```yaml
    users:
      - name: xr-rollout-promoter
        user:
          token: <bearer-token>
```

If you use External Secrets Operator, this object writes the Secret above. `vault` is a stand-in SecretStore name. The template sets the label and the annotation on the Secret.

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
