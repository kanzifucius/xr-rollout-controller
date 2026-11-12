# Environment gate

The same `CompositionRollout` manifest can be synced to every cluster. Each controller knows its own environment from the process flag `--environment` (Helm value `environment`, default `dev`).

After soak, the controller looks up `spec.promotion.environments[<flag>]`:

| Policy | After a healthy soak |
|--------|----------------------|
| `AutoPromote` | Enter `Promoting` and start waves. |
| `ManualApproval` | Enter `AwaitingApproval` and wait for an `Approve` trigger bound to this revision. |

If the flag's name is missing from the map, `defaultPolicy` applies. That field defaults to `ManualApproval`, so an unknown environment waits for a human.

```yaml
promotion:
  defaultPolicy: ManualApproval
  environments:
    dev: AutoPromote
    test: AutoPromote
    acceptance: ManualApproval
    prod: ManualApproval
```

A cluster installed with `--set environment=prod` waits. A cluster installed with `--set environment=dev` promotes on its own. The manifest does not change.

`AwaitingApproval` is also a status condition on the `CompositionRollout`, so a release process can watch one object instead of the whole fleet.

The approval itself is a `spec.triggers` entry, not an annotation. See [Triggers](triggers.md).

Cross-cluster order is a different mechanism from `spec.promotion.environments`. The environment map still decides `AutoPromote` or `ManualApproval` inside the cluster after soak. See [Promotion across clusters](promotion.md).
