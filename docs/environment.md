# Environment gate

After soak, the controller looks up `spec.promotion.environments[<environment>]`.

| Policy | After a healthy soak |
|--------|----------------------|
| `AutoPromote` | Enter `Promoting` and start waves. |
| `ManualApproval` | Enter `AwaitingApproval` and wait for an `Approve` trigger bound to this revision. |

If that name is missing from the map, `defaultPolicy` applies. That field defaults to `ManualApproval`, so an unknown environment waits for a human.

The environment name depends on the mode:

| Mode | Where the name comes from |
|------|---------------------------|
| `local` | This pod's `--environment` (Helm `environment`, default `dev`). |
| `remote` | The spoke pod's `--environment`. The management install only appends `Start` and does not apply this gate. |
| `hub` | The annotation `rollouts.platform.example.org/environment` on the kubeconfig Secret. A missing or empty annotation does not patch and does not write status. The hub pod's `--environment` is not the key. |

The same manifest can be synced everywhere. A spoke installed with `--set environment=prod`, or a hub Secret annotated `prod`, waits when the map below says `prod: ManualApproval`. A `dev` install promotes on its own.

```yaml
promotion:
  defaultPolicy: ManualApproval
  environments:
    dev: AutoPromote
    test: AutoPromote
    acceptance: ManualApproval
    prod: ManualApproval
```

`AwaitingApproval` is also a status condition on the `CompositionRollout`, so a release process can watch one object instead of the whole fleet.

The approval itself is a `spec.triggers` entry, not an annotation. See [Triggers](triggers.md).

Cross-cluster order is a different mechanism from `spec.promotion.environments`. The environment map still decides `AutoPromote` or `ManualApproval` inside the cluster after soak. See [Promotion across clusters](promotion.md).
