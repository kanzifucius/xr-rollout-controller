# Dry run

`spec.dryRun: true` runs the rollout decisions and does not change composite resources (XRs). The controller still chooses the canary, waits out soak and wave timers, stops for the environment gate, and pauses for the same health reasons. It records the XRs it would have patched. It does not write `spec.crossplane.compositionRevisionRef` or `compositionUpdatePolicy`.

Leave the field off, or set it to `false`, for a real rollout. The default is `false`. One `CompositionRollout` can be a dry run while another object on the same controller still patches.

In hub mode the dry run lists with the kubeconfig named on that object. A missing Secret pauses that object and leaves `wouldUpdate` empty.

```yaml
spec:
  dryRun: true
  autoStart: false
```

`autoStart: false` keeps a later edit that clears `dryRun` from starting a real rollout on its own. With `autoStart: true`, the XRs are still on the old revision, so the next reconcile after the flag is cleared can start a real rollout. `spec.suspend: true` still pauses the object and ignores triggers.

## What you can read

`status.rollouts[].canary.selected` is the frozen canary set, as on a real rollout.

`status.rollouts[].dryRun` is the simulation:

| Field | Meaning |
|---|---|
| `active` | `spec.dryRun` is true. |
| `promotion` | The policy for this cluster's `--environment`. |
| `wouldUpdate` | Manual XRs that would move, in patch order. `stage` is `Canary` or `Wave`. |
| `completedAt` | When the simulation finished. |

`status.rollouts[].completedAt` stays empty. The phase does not become `Completed`, and the `Succeeded` condition stays False. A finished simulation sits at `Idle` with `dryRun.completedAt` set.

`updatedXRs` counts XRs whose revision ref already equals the target. A dry run does not increase it. `xr_rollout_updates_total` does not increase either. The phase gauge shows the simulated phase.

Triggers are acknowledged in `acknowledgedTriggers` and are not removed from `spec.triggers`. `Approve`, `Resume`, and `Abort` still have to name the target revision.

Health is the XR's current `Ready` and `Synced` conditions. The dry run does not apply the new CompositionRevision, so those conditions are not evidence about that revision. A canary that is not `Ready` and `Synced` pauses the simulation. XRs that were already recorded in `wouldUpdate` stay there. Nothing is rolled back.

Clearing `spec.dryRun` drops the simulation report and returns that composition to `Idle` without patching. Acknowledged triggers and `abortedRevision` remain. The following reconcile is an ordinary idle rollout.

## Promoter

A dry run does not open the next cluster. The promoter leaves the hop `Blocked` with reason `DryRun` when either the predecessor or the next `CompositionRollout` is a dry run. It does not append `Start`. `dryRun.completedAt` is not a completed revision.

Set `spec.dryRunChain: true` on the `PromotionPipeline` to record the `Start` that a finished dry-run predecessor would have written. The record is `status.hops[].wouldStart`. The promoter still does not write it, and a dry run still does not set `completedAt`, publish phase `Completed`, or patch XRs. Leave `dryRunChain` unset to keep the hop `Blocked`.

## Trying it

Set `dryRun: true` and a short `soak.duration` on a copy of the object you want to test. Watch:

```bash
kubectl get compositionrollouts.rollouts.platform.example.org -o wide
kubectl get compositionrollout team-payments -o jsonpath='{.status.rollouts[0].dryRun}'
```

Confirm an XR revision ref is unchanged:

```bash
kubectl get <xr-plural> -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name} {.spec.crossplane.compositionRevisionRef.name}{"\n"}{end}'
```

Use a real `CompositionRollout` (`dryRun: false`) when you want the revision ref to change.
