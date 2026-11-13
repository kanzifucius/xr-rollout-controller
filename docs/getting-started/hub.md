# Hub

Remote and hub use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. It is not a GA multi-cluster API. Engagement, kubeconfig Secrets, and the hub layout can change. [Local](local.md) mode does not use it.

Pick hub when you want one `CompositionRollout` per cluster on the management cluster, and no controller on the spokes.

The object and its status stay here. `spec.cluster` names the spoke. This process patches `spec.crossplane.compositionRevisionRef.name` on Manual XRs on that spoke. It does not write `compositionUpdatePolicy`, and it does not roll a revision back. A second object with the same `spec.cluster` pauses.

## What you get

`kubectl get compositionrollout` on the hub is the health of every cluster. Spokes do not need this CRD, this Deployment, or a pod `--environment`. Metrics are scraped here. The rollout label is `<spec.cluster>/<object name>`, for example `acc/team-payments`.

The kubeconfig user on the spoke can patch the XR types you aggregate. The process only writes the revision name, and only on Manual XRs. Names on the hub are unique, so you cannot call two objects `team-payments`. `spec.cluster` is how you tell `dev` from `acc`. This install depends on the alpha library above.

## Management cluster

Set `mode: hub`. One Secret per cluster, in the release namespace, with the same label and annotation as [remote](remote.md). The [install](../install.md) example uses `xr-rollout-controller-system`. The Secret name is the cluster name. The annotation `rollouts.platform.example.org/environment` is the promotion key, so `acc` can be `acceptance`. Local mode does not use this Secret. Remote and hub both do. The chart does not create it.

The ServiceAccount can update `CompositionRollout` and `PromotionPipeline`, including status. The chart does not bind the XR aggregate role here. The XRs are on the spokes.

An empty `spec.cluster` is an error. Nothing is patched and status is left alone until you set it.

`Start` for the next cluster is appended to the hub object for that `spec.cluster`, still only `Start`, field manager `xr-rollout-promoter`.

## Each spoke

Crossplane and the XRs. Apply `config/rbac/hub_spoke_role.yaml` (copied to `charts/xr-rollout-controller/files/hub-spoke-rbac.yaml`). Do not install the `CompositionRollout` CRD or the controller.

ClusterRole `xr-rollout-hub` can read Compositions and CompositionRevisions. It cannot update a `CompositionRollout`.

ClusterRole `xr-rollout-hub-xr-access` aggregates ClusterRoles labelled `rollouts.platform.example.org/aggregate-to-rollout-controller: "true"` and is bound to User `xr-rollout-hub`. The user name has to match the kubeconfig user. `aggregationRule` is a top-level field. Copy `config/rbac/xr_aggregate_role.yaml` once per XR API group and set that label.

Do not also run a spoke controller. Both would write the same XR field as `xr-rollout-controller`.

## Example

```yaml
mode: hub
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

The user in the kubeconfig is `xr-rollout-hub`, the User bound to the spoke XR role. The client certificate and key are that user's credentials.

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
          user: xr-rollout-hub
    current-context: acc
    users:
      - name: xr-rollout-hub
        user:
          client-certificate-data: <client-certificate-data>
          client-key-data: <client-key-data>
```

```yaml
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-payments
spec:
  cluster: acc
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

`dev` is a second object, for example `team-payments-dev`, with `spec.cluster: dev` and `autoStart: true`.
