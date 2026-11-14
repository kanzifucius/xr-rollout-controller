# Hub

Remote and hub use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. It is not a GA multi-cluster API. Engagement, kubeconfig Secrets, and the hub layout can change. [Local](local.md) mode does not use it.

Pick hub when you want `CompositionRollout` objects on the management cluster, and no controller on the spokes.

The object and its status stay here. `spec.cluster` is the logical cluster. This process patches `spec.crossplane.compositionRevisionRef.name` on Manual XRs on that spoke, using the kubeconfig that object names. It does not write `compositionUpdatePolicy`, and it does not roll a revision back. Two objects may share `spec.cluster` when they govern different compositions. Two objects that claim the same composition pause.

Per-rollout kubeconfig is hub mode only. Local mode has no kubeconfig Secrets. Remote mode keeps one Secret per cluster and one ServiceAccount on each spoke.

## What you get

`kubectl get compositionrollout` on the hub is the health of every cluster. Spokes do not need this CRD, this Deployment, or a pod `--environment`. Metrics are scraped here. The rollout label is `<spec.cluster>/<object name>`, for example `acc/team-payments`.

The kubeconfig user on the spoke can patch the XR types that user is bound to. The process only writes the revision name, and only on Manual XRs. Names on the hub are unique, so you cannot call two objects `team-payments`. `spec.cluster` is how you tell `dev` from `acc`. This install depends on the alpha library above.

## Management cluster

Set `mode: hub`. Kubeconfig Secrets live in the release namespace, with the same label as [remote](remote.md). The [install](../install.md) example uses `xr-rollout-controller-system`. The chart does not create them.

When `spec.kubeconfigSecretRef` is unset, the Secret name is `spec.cluster`. That is the single-Secret layout below.

A Secret may set `rollouts.platform.example.org/cluster` to the logical cluster. Several Secrets can then share one `spec.cluster`, and each Secret name is a separate client. `spec.cluster` stays the promotion key. The promotion pipeline does not choose the credential.

`rollouts.platform.example.org/environment` is the promotion environment, so `acc` can be `acceptance`. A missing or empty environment annotation does not patch and does not write status. Local mode does not use this Secret. Remote and hub both do.

If the named Secret is missing, or its kubeconfig will not parse, that rollout pauses with `CredentialUnavailable`. The pause clears on the next reconcile after the Secret engages. Other rollouts on the same cluster keep going. If the kubeconfig user cannot patch `compositionRevisionRef`, that rollout pauses with `CredentialDenied` until `Resume`.

`spec.dryRun: true` lists with that same client and does not patch. A missing credential pauses the dry run and does not record `wouldUpdate`.

The ServiceAccount can update `CompositionRollout` and `PromotionPipeline`, including status. The chart does not bind the XR aggregate role here. The XRs are on the spokes.

An empty `spec.cluster` is an error. Nothing is patched and status is left alone until you set it.

`Start` for the next cluster is appended to the hub object for that `spec.cluster` and composition, still only `Start`, field manager `xr-rollout-promoter`.

## Each spoke

Crossplane and the XRs. Apply `config/rbac/hub_spoke_role.yaml` (copied to `charts/xr-rollout-controller/files/hub-spoke-rbac.yaml`). Do not install the `CompositionRollout` CRD or the controller.

ClusterRole `xr-rollout-hub` can read Compositions and CompositionRevisions. It cannot update a `CompositionRollout`.

ClusterRole `xr-rollout-hub-xr-access` aggregates ClusterRoles labelled `rollouts.platform.example.org/aggregate-to-rollout-controller: "true"` and is bound to User `xr-rollout-hub`. The user name has to match the kubeconfig user. `aggregationRule` is a top-level field. Copy `config/rbac/xr_aggregate_role.yaml` once per XR API group and set that label. Keep this binding when one user may patch every XR type.

For a team split, bind each kubeconfig user only to that team's XR types. The sample is `config/rbac/hub_spoke_team_role.yaml` (copied to `charts/xr-rollout-controller/files/hub-spoke-team-rbac.yaml`). It is a narrowed ClusterRole per team, plus the composition read role. Do not bind `xr-rollout-hub-xr-access` to those users. The hub ServiceAccount does not get either spoke binding.

Do not also run a spoke controller. Both would write the same XR field as `xr-rollout-controller`.

## Example

```yaml
mode: hub
promoter:
  clusters:
    - dev
    - acc
```

Apply this ExternalSecret, or apply the Secret it writes. `vault` is an example SecretStore. The template sets the label and the annotation on the Secret. The Secret name is the cluster name, and the CompositionRollout below leaves `spec.kubeconfigSecretRef` unset.

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

## Two teams, one cluster

`acc-payments` and `acc-platform` are two Secrets for cluster `acc`. Each user is bound only to that team's XR types. Each CompositionRollout sets `spec.cluster: acc` and names its own Secret. This is hub mode only.

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: acc-payments
  namespace: xr-rollout-controller-system
spec:
  secretStoreRef:
    name: vault
    kind: SecretStore
  target:
    name: acc-payments
    template:
      metadata:
        labels:
          sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
        annotations:
          rollouts.platform.example.org/cluster: acc
          rollouts.platform.example.org/environment: acceptance
  data:
    - secretKey: kubeconfig
      remoteRef:
        key: clusters/acc/payments
        property: kubeconfig
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: acc-platform
  namespace: xr-rollout-controller-system
spec:
  secretStoreRef:
    name: vault
    kind: SecretStore
  target:
    name: acc-platform
    template:
      metadata:
        labels:
          sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
        annotations:
          rollouts.platform.example.org/cluster: acc
          rollouts.platform.example.org/environment: acceptance
  data:
    - secretKey: kubeconfig
      remoteRef:
        key: clusters/acc/platform
        property: kubeconfig
```

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: acc-payments
  namespace: xr-rollout-controller-system
  labels:
    sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
  annotations:
    rollouts.platform.example.org/cluster: acc
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
          user: xr-rollout-payments
    current-context: acc
    users:
      - name: xr-rollout-payments
        user:
          client-certificate-data: <client-certificate-data>
          client-key-data: <client-key-data>
---
apiVersion: v1
kind: Secret
metadata:
  name: acc-platform
  namespace: xr-rollout-controller-system
  labels:
    sigs.k8s.io/multicluster-runtime-kubeconfig: "true"
  annotations:
    rollouts.platform.example.org/cluster: acc
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
          user: xr-rollout-platform
    current-context: acc
    users:
      - name: xr-rollout-platform
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
  kubeconfigSecretRef:
    name: acc-payments
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
---
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-platform
spec:
  cluster: acc
  kubeconfigSecretRef:
    name: acc-platform
  compositions:
    - name: platformcache.example.org
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

Leave `kubeconfigSecretRef.namespace` empty. It defaults to the controller namespace, which is the release namespace. A payments identity that cannot see platform XRs still rolls `platformdb.example.org`, because that object lists and patches with `acc-payments` only.
