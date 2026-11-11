# Architecture

The controller is one manager process. It watches `CompositionRollout`, `CompositionRevision`, and the composite resource types those compositions produce, then patches Manual XRs onto a target revision.

## Diagrams

Interactive **Archify** HTML is embedded on each caption page. Open the standalone file for a full browser tab.

| Diagram | Page | Standalone HTML |
|---------|------|-----------------|
| Runtime | [Runtime diagram](runtime.md) | [xr-rollout-runtime.html](xr-rollout-runtime.html) |
| Canary, soak, and promotion | [Rollout flow](workflow.md) | [rollout-flow.html](rollout-flow.html) |
| Approve trigger | [Approve sequence](sequence.md) | [approve-sequence.html](approve-sequence.html) |

The diagrams are generated from repository-backed Archify candidates under `.archify/` (not committed). Pin evidence with `--repo-root` and `--quality showcase`.

## What the pages leave in prose

- [The CRD](../crd.md) lists every spec and status field.
- [Canary and promotion](../canary.md) covers selection, soak, and waves.
- [Triggers](../triggers.md) covers Start, Resume, and Abort as well as Approve.

The design note in git is [`docs/xr-rollout-controller-plan.md`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/docs/xr-rollout-controller-plan.md).
