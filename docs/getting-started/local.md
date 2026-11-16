# Local mode

Pick local when the controller and the composite resources (XRs) are on the same cluster. That is the default, and the only mode that does not use the alpha multi-cluster library. It is also the install you put on each spoke when a management cluster runs [remote mode](remote.md).

One process watches `CompositionRollout` on that cluster and patches Manual XRs there. It does not talk to another cluster.

## What you get

Health is `kubectl get compositionrollout` on that cluster. The controller has no kubeconfig for anywhere else.

Every cluster that rolls its own XRs needs its own controller. If you want one install to open the next cluster, use [remote mode](remote.md) or [hub mode](hub.md).

## What is installed

One cluster. The chart installs the `CompositionRollout` CRD, the controller, and RBAC. Crossplane and the XRs are already there. There is no second cluster in this layout.

## RBAC

The chart binds the manager ClusterRole to the controller ServiceAccount. That account can update `CompositionRollout`, including status, and it can read Compositions and CompositionRevisions.

XR access is a second ClusterRole, `<release>-xr-access`. `aggregationRule` sits at the top level of that role, not under `metadata`. It selects every ClusterRole labelled:

```yaml
rollouts.platform.example.org/aggregate-to-rollout-controller: "true"
```

Label one ClusterRole per XR API group. The chart binds the aggregate role to the same ServiceAccount. `config/rbac/xr_aggregate_role.yaml` is the example. The only XR write is a server-side apply of `spec.crossplane.compositionRevisionRef.name` on Manual XRs, field manager `xr-rollout-controller`.

## Example

```yaml
mode: local
environment: dev
```

```bash
helm upgrade --install xr-rollout-controller \
  oci://ghcr.io/kanzifucius/charts/xr-rollout-controller \
  --version 1.2.3 \
  --namespace xr-rollout-controller-system \
  --create-namespace \
  --set mode=local \
  --set environment=dev
```

Replace `1.2.3` with the chart version. `environment` here is the key in `spec.promotion.environments`.

```yaml
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-payments
spec:
  compositions:
    - name: platformdb.example.org
  autoStart: true
  canary:
    strategy: Percent
    percent: 5
  soak:
    duration: 1h
  promotion:
    defaultPolicy: ManualApproval
    environments:
      dev: AutoPromote
  waves:
    batchSize: "10%"
```

Leave `spec.cluster` empty. The object belongs on this cluster.
