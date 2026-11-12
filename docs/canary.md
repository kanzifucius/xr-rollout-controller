# Canary and promotion

Each composition on a `CompositionRollout` runs its own state machine.

```text
Idle → Canarying → Soaking → AwaitingApproval or Promoting → Completed → Idle
                 ↘ Paused ← Soaking / Promoting
                   Resume returns to the phase that paused
                   Abort → Aborted → Idle (no rollback)
```

`autoStart: true` (the default) enters `Canarying` when a newer revision appears. `onNewRevision: Restart` drops an in-flight target and starts again on the newest revision. `Queue` finishes the current rollout first.

## Canary selection

`spec.canary.strategy` is one of:

| Strategy | Behaviour |
|----------|-----------|
| `LabelSelector` | The XRs that match [`canary.labelSelector`](#labelselector-strategy) are the canary set. An empty match pauses with `CanarySetEmpty`. |
| `Count` | A fixed number of XRs. |
| `Percent` | A percentage of the fleet, rounded up, minimum 1. |

`Count` and `Percent` order candidates by a hash of the XR UID, with namespace and name as the tie-break, so the same XRs are chosen across restarts. `preferredNamespaces` draws from those namespaces first, in list order, and is ignored for `LabelSelector`. Those two strategies do not read `canary.labelSelector`.

The chosen set is stored in `status.rollouts[].canary.selected` when canarying starts and is not recomputed for that rollout. A deleted canary shrinks the set. If the set becomes empty during soak, the rollout pauses.

## LabelSelector strategy

`canary.labelSelector` is used only when `canary.strategy` is `LabelSelector`. It is the same standard selector as [`xrSelector.labelSelector`](crd.md#which-xrs-a-composition-governs): `matchLabels` and `matchExpressions` are ANDed, and the operators are `In`, `NotIn`, `Exists`, and `DoesNotExist`.

The governed XRs that match become the canary set. `preferredNamespaces` is ignored. Leaving `labelSelector` unset, or setting an empty selector, matches every governed XR. A selector that matches nobody pauses the rollout with `CanarySetEmpty`.

```yaml
canary:
  strategy: LabelSelector
  labelSelector:
    matchExpressions:
      - key: rollout.example.org/canary
        operator: In
        values: ["true"]
```

A manifest that sets both the fleet selector and this canary selector is in [Fleet and canary together](crd.md#fleet-and-canary-together).

Canaries are patched with server-side apply, field manager `xr-rollout-controller`, changing only `spec.crossplane.compositionRevisionRef.name`. Crossplane's own field manager on that field is taken so a Manual rollout can proceed. A conflict with any other manager pauses with `PatchFailed` unless the Deployment sets `--force-ownership`.

A dry run records those patches in `status.rollouts[].dryRun.wouldUpdate` and does not apply them. See [Dry run](dry-run.md).

## Soak

`spec.soak.duration` (default `30m`) is how long every canary must hold `Ready=True` and `Synced=True`. Health means the conditions are currently `True` and their `lastTransitionTime` is at or before the soak start, so a flip to `False` during the window fails that XR even if it recovers.

`maxUnhealthy` (default `0`) is how many unhealthy canaries are tolerated. `stabilisationTimeout` (default `30m`) bounds how long the controller waits for canaries to become healthy the first time. Exceeding either pauses the rollout. Already-updated XRs stay on the new revision.

## Waves

After the gate, the remainder moves in waves. `waves.batchSize` is an integer or a percentage string such as `"10%"`. `maxConcurrentUpdates` (default 20) caps in-flight patches. `waveSoak` (default `5m`) is the health hold between waves, with its own `maxUnhealthy` (default `0`).

A failed wave pauses with `WaveUnhealthy`. When every wave is healthy the phase becomes `Completed`, then `Idle`.

## Pause

While `Paused`, the controller writes no further revision refs for that composition. `spec.suspend: true` pauses everything with reason `Suspended` and ignores triggers until it is cleared.

There is no automatic rollback. [Triggers](triggers.md) covers Resume and Abort. The diagram of this path is [Rollout flow](architecture/workflow.md).
