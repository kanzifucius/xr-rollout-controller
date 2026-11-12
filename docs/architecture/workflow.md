# Rollout flow

One composition, from a new revision through canary, soak, the environment gate, and waves. The pause branch is the unhealthy soak. Canary and wave failures pause the same way; Resume and Abort are described beside the diagram.

<div class="archify-embed" markdown="0">
<iframe src="../rollout-flow.html" title="Canary, soak, and promotion flow" loading="lazy" referrerpolicy="no-referrer"></iframe>
</div>

<p class="archify-caption">
  autoStart or Start freezes a canary and patches it; a held health window opens the environment gate;
  AutoPromote or Approve starts waves; Completed returns to Idle.
  <a href="../rollout-flow.html">Open standalone HTML</a>.
</p>

A dry run follows this order and skips the patch. It does not publish `Completed`. See [Dry run](../dry-run.md).

Field-level behaviour: [Canary and promotion](../canary.md) and [Environment gate](../environment.md).
