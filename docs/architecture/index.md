# Architecture

These diagrams are one controller applying a rollout: watch `CompositionRollout` and `CompositionRevision`, then patch Manual XRs. That is [local mode](../getting-started/local.md), and it is also what [hub mode](../getting-started/hub.md) does to spoke XRs from the management cluster. [Remote mode](../getting-started/remote.md) does not patch XRs. The management install appends `Start`, and each spoke runs the local controller drawn here.

## Diagrams

Interactive **Archify** HTML is embedded on each caption page. Open the standalone file for a full browser tab.

| Diagram | Page | Standalone HTML |
|---------|------|-----------------|
| Runtime | [Runtime diagram](runtime.md) | [xr-rollout-runtime.html](xr-rollout-runtime.html) |
| Canary, soak, and promotion | [Rollout flow](workflow.md) | [rollout-flow.html](rollout-flow.html) |
| Approve trigger | [Approve sequence](sequence.md) | [approve-sequence.html](approve-sequence.html) |

The diagrams are generated from repository-backed Archify candidates under `.archify/` (not committed). Pin evidence with `--repo-root` and `--quality showcase`.

## What the pages leave in prose

- [CompositionRollout](../crd.md) lists every spec and status field.
- [Canary, soak, and waves](../canary.md) covers selection, soak, and waves.
- [Triggers](../triggers.md) covers Start, Resume, and Abort as well as Approve.

The design note in git is [`docs/xr-rollout-controller-plan.md`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/docs/xr-rollout-controller-plan.md).
