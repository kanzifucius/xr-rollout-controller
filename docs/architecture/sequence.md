# Approve sequence

A release manager adds an `Approve` trigger. Admission checks the group, the reconciler asks the state machine, and a matching revision moves the composition to `Promoting`.

<div class="archify-embed" markdown="0">
<iframe src="../approve-sequence.html" title="Approve trigger sequence" loading="lazy" referrerpolicy="no-referrer"></iframe>
</div>

<p class="archify-caption">
  Approve is admitted for <code>rollouts.platform.example.org:release-managers</code>,
  applied only in <code>AwaitingApproval</code> for the target revision, then acknowledged on status.
  <a href="../approve-sequence.html">Open standalone HTML</a>.
</p>

Start, Resume, and Abort use the same trigger list. See [Triggers](../triggers.md).
