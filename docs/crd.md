# CompositionRollout

`CompositionRollout` is a cluster-scoped custom resource (`rollouts.platform.example.org/v1alpha1`). The short name is `xrollout`. One object covers a team's compositions. Each composition has its own revision stream and its own phase.

XRs are in scope when `spec.crossplane.compositionRef.name` matches a listed composition and `compositionUpdatePolicy` is `Manual`. An optional `xrSelector` narrows that set by namespace or labels.

## Spec

| Field | Role |
|-------|------|
| `compositions` | Composition names to govern. At least one. Optional [`xrSelector`](#which-xrs-a-composition-governs) narrows the fleet. |
| `suspend` | Pauses every in-flight rollout and ignores new revisions until cleared. |
| `autoStart` | Start a rollout when a newer revision appears. Default `true`. |
| `onNewRevision` | `Restart` (default) abandons the current target for a newer revision. `Queue` finishes the current rollout first. |
| `canary` | How the first subset is chosen. See [Canary and promotion](canary.md). |
| `soak` | How long canaries must stay healthy. |
| `promotion` | Per-environment policy after soak. See [Environment gate](environment.md). |
| `waves` | Batch size, concurrency, and the pause between waves. |
| `triggers` | Manual `Start`, `Approve`, `Resume`, and `Abort`. See [Triggers](triggers.md). |

The controller does not write `compositionUpdatePolicy`, `compositionRef`, labels, or any schema field on the XR. Pinning XRs to Manual is done by the XR author or by the XRD's `defaultCompositionUpdatePolicy`.

## Label selectors

`compositions[].xrSelector.labelSelector` and `canary.labelSelector` are both a standard Kubernetes label selector. `matchLabels` and `matchExpressions` are ANDed. Each `matchLabels` entry is an `In` match on that single value. `matchExpressions[].operator` is `In`, `NotIn`, `Exists`, or `DoesNotExist`. `In` and `NotIn` take a non-empty `values` list. `Exists` and `DoesNotExist` take an empty one.

### Which XRs a composition governs

`xrSelector` is optional and sits on top of the composition ref. A Manual XR is in that composition's fleet only when:

- `spec.crossplane.compositionRef.name` equals `compositions[].name`
- `compositionUpdatePolicy` is `Manual`
- `xrSelector.namespaces`, when set, includes the XR's namespace
- `xrSelector.labelSelector`, when set, matches the XR's labels

`namespaces` is the other optional narrow. Names in the list are alternatives. The label selector is applied as well, and both have to match. Omit `xrSelector`, or omit one of its fields, and that narrow is not applied. A cluster-scoped XR has an empty namespace, so a `namespaces` list leaves it out.

```yaml
compositions:
  - name: platformdb.example.org
    xrSelector:
      namespaces: [payments-prod]
      labelSelector:
        matchLabels:
          team: payments
        matchExpressions:
          - key: rollout.example.org/skip
            operator: DoesNotExist
```

That keeps Manual `platformdb.example.org` XRs in `payments-prod` with `team=payments` and without `rollout.example.org/skip`.

### Fleet and canary together

`canary.labelSelector` does not change the fleet. It chooses the canary set, and only when `canary.strategy` is `LabelSelector`. The strategy is documented in [LabelSelector strategy](canary.md#labelselector-strategy).

```yaml
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-payments
spec:
  compositions:
    - name: platformdb.example.org
      xrSelector:
        namespaces: [payments-dev, payments-prod]
        labelSelector:
          matchLabels:
            team: payments
          matchExpressions:
            - key: env
              operator: In
              values: [dev, prod]
  canary:
    strategy: LabelSelector
    labelSelector:
      matchLabels:
        rollout.example.org/canary: "true"
  soak:
    duration: 30m
  promotion:
    defaultPolicy: ManualApproval
  waves:
    batchSize: "10%"
```

The fleet is Manual `platformdb.example.org` XRs in `payments-dev` or `payments-prod`, with `team=payments` and `env` of `dev` or `prod`. The canary set is those XRs that also have `rollout.example.org/canary=true`. If none do, the rollout pauses with `CanarySetEmpty`.

## Sample

```yaml
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-payments
spec:
  compositions:
    - name: platformdb.example.org
    - name: platformcache.example.org
      xrSelector:
        namespaces: [payments-dev, payments-prod]
  autoStart: true
  onNewRevision: Restart
  canary:
    strategy: Percent
    percent: 5
    preferredNamespaces: [payments-dev]
  soak:
    duration: 1h
    maxUnhealthy: 0
    stabilisationTimeout: 30m
  promotion:
    defaultPolicy: ManualApproval
    environments:
      dev: AutoPromote
      test: AutoPromote
      acceptance: ManualApproval
      prod: ManualApproval
  waves:
    batchSize: "10%"
    maxConcurrentUpdates: 25
    waveSoak: 10m
```

The repository sample is [`config/samples/rollouts_v1alpha1_compositionrollout.yaml`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/config/samples/rollouts_v1alpha1_compositionrollout.yaml).

## Status

Top-level conditions aggregate every composition: `Progressing`, `AwaitingApproval`, `Paused`, and `Succeeded`.

`status.rollouts[]` is one entry per composition:

| Field | Role |
|-------|------|
| `phase` | `Idle`, `Canarying`, `Soaking`, `AwaitingApproval`, `Promoting`, `Paused`, `Completed`, `Aborted` |
| `targetRevision` | CompositionRevision name and `spec.revision` number being rolled out |
| `totalXRs`, `updatedXRs`, `healthyXRs`, `unmanagedXRs` | Fleet accounting. Unmanaged includes Automatic XRs. |
| `canary.selected` | Frozen canary members (`namespace` / `name`) |
| `wave` | Current wave, total waves, and when the wave soak ends |
| `pauseReason` | Machine-readable reason while `Paused` |
| `acknowledgedTriggers` | Triggers already consumed, so each entry acts once |

`kubectl get compositionrollouts` prints the first composition's phase.

Two `CompositionRollout` objects must not claim the same composition. The example admission policy rejects the overlap, and the controller still pauses both if admission is bypassed.
