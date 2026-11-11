# Triggers

`spec.triggers` is the manual control surface. The controller never deletes or edits spec (GitOps would fight that). It records consumption on `status.rollouts[].acknowledgedTriggers`. A trigger already acknowledged for the same composition, action, and revision is a no-op.

Each entry names the composition, the action, an optional revision, and an optional `actor` string for audit.

| Action | When it applies | Revision |
|--------|-----------------|----------|
| `Start` | Phase is `Idle`. Begins canarying the latest revision, or the named revision when it is that latest revision. | Optional. An explicit name must be the latest revision. |
| `Approve` | Phase is `AwaitingApproval`. Enters `Promoting`. | Required, and must equal the target revision. |
| `Resume` | Phase is `Paused`. Returns to the phase stored in `phaseBeforePause` (or `Canarying` if that is empty) and clears the pause reason. | Required, and must equal the target revision. |
| `Abort` | Phase is `AwaitingApproval` or `Paused`. Moves to `Aborted`, then `Idle`. | Required, and must equal the target revision. |

A trigger that does not match the phase or revision is acknowledged with outcome `Stale` and does not change the phase. `autoStart` will not immediately start a rollout for a revision that was aborted. A later `Start` can run that revision again from `Canarying`.

Abort does not revert XRs. Canaries already on the new revision stay there. The rest stay on the old one until a later rollout moves them.

```yaml
spec:
  triggers:
    - composition: platformdb.example.org
      action: Approve
      revision: platformdb.example.org-9f8a2c1
      actor: ricardo@example.org
```

## Admission

The chart's ValidatingAdmissionPolicy allows `Start` and `Abort` for any caller who can update the object. `Approve` and `Resume` require the group `rollouts.platform.example.org:release-managers`. Approve, Resume, and Abort must set `revision`.

Disable the policy with `--set admissionPolicy.enabled=false` only when another control covers that group.

The message order for a successful Approve is [Approve sequence](architecture/sequence.md).
