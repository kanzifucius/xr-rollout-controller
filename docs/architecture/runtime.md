# Runtime diagram

How an install reaches the reconciler, how a new CompositionRevision wakes it, and the single field written on Manual XRs.

<div class="archify-embed" markdown="0">
<iframe src="../xr-rollout-runtime.html" title="CompositionRollout runtime architecture" loading="lazy" referrerpolicy="no-referrer"></iframe>
</div>

<p class="archify-caption">
  Operator installs the public OCI chart; the manager reconciles each composition;
  revisions enqueue on create; server-side apply writes <code>compositionRevisionRef</code>;
  admission guards Approve and Resume on the rollout object.
  <a href="../xr-rollout-runtime.html">Open standalone HTML</a>.
</p>

The Helm values and image tags are in [Install](../install.md).
