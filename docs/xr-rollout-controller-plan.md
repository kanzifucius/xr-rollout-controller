# XR Rollout Controller — Design Plan

A Kubebuilder-based Kubernetes controller that safely rolls out Crossplane **CompositionRevision** upgrades to composite resources (XRs), using canaries, soak-based health checks, per-environment promotion gates, and batched waves.

Target platform: **Crossplane v2 only** (no claims; XRs may be namespaced or cluster-scoped).

---

## 1. Overview, goals and non-goals

### Background: Crossplane v2 revision mechanics

The design relies on the following upstream behaviour, per the [Crossplane v2 Composition Revisions docs](https://docs.crossplane.io/latest/composition/composition-revisions/) and the [XRD docs](https://docs.crossplane.io/latest/composition/composite-resource-definitions/):

- Every edit to a `Composition` produces an immutable, cluster-scoped `CompositionRevision` named `<composition>-<hash>`, labelled `crossplane.io/composition-name: <composition>`, and carrying a monotonically increasing `spec.revision` number that identifies the newest revision.
- XRs carry `spec.crossplane.compositionRef`, `spec.crossplane.compositionUpdatePolicy` (`Automatic` default, or `Manual`) and `spec.crossplane.compositionRevisionRef`.
- Crossplane sets `compositionRevisionRef` to the **latest** revision at XR creation time *regardless* of policy. Under `Automatic`, Crossplane's composite controller keeps bumping the ref to the latest revision itself. Under `Manual`, the ref stays where it is until something edits it — that something is this controller.
- An XRD can set `spec.defaultCompositionUpdatePolicy: Manual` so all new XRs of that kind are born pinned.
- XRs expose the standard Crossplane `Ready` and `Synced` status conditions.
- `Automatic` XRs may additionally use `compositionRevisionSelector.matchLabels` to track a labelled channel; that mechanism is orthogonal to (and not used by) this controller.

### Goals

- Upgrade fleets of `Manual`-policy XRs to a new `CompositionRevision` in a controlled sequence: canary → soak → gate → batched promotion.
- One team-owned config object governs a set of compositions and the XRs that reference them.
- Rollouts start automatically on a new revision and can be triggered manually.
- Deterministic, stable canary selection (label selector, fixed count, or percentage).
- Health = XR `Ready` and `Synced` conditions held `True` over a configurable soak period.
- Per-environment promotion behaviour (dev/test/acceptance/prod): auto-promote or halt for explicit approval.
- A failed canary pauses the rollout; humans resume or abort. No automatic rollback.
- Scales to large fleets: event-driven watches and field indexes, no per-instance polling; the remainder rolls in bounded waves.

### Non-goals

- **XRD schema version changes.** Only revision bumps within the same composite schema are in scope. A revision whose `spec.compositeTypeRef` differs from the XR's current apiVersion is never applied.
- Automatic rollback of a failed canary or wave.
- Managing XRs with `compositionUpdatePolicy: Automatic` (see §5 for coexistence).
- Rendering/diffing compositions pre-rollout — decided: that lives in CI on the Composition repo (§11).
- Writing `compositionUpdatePolicy` on XRs — no adoption mode; pinning is done by XR authors or the XRD's `defaultCompositionUpdatePolicy` (§11).
- Multi-cluster orchestration. The controller is per-cluster; cross-cluster sequencing is done by syncing the same config to clusters whose controllers run with different environment settings.

---

## 2. CRD API design

One new CRD: **`CompositionRollout`** (group `rollouts.platform.example.org/v1alpha1`, cluster-scoped — compositions and revisions are cluster-scoped, and a team's XRs may span namespaces). One object per team, covering that team's set of compositions.

### Go type sketches

```go
// CompositionRolloutSpec defines how revisions of a set of compositions are
// rolled out to the XRs that reference them.
type CompositionRolloutSpec struct {
	// Compositions governed by this rollout config. XRs are selected by
	// spec.crossplane.compositionRef.name matching one of these entries and
	// compositionUpdatePolicy == Manual.
	// +kubebuilder:validation:MinItems=1
	Compositions []CompositionTarget `json:"compositions"`

	// Suspend stops the controller acting on new revisions (and pauses any
	// in-flight rollout) without deleting state.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// AutoStart begins a rollout whenever a newer CompositionRevision appears.
	// When false, rollouts only start via the manual trigger. Default true.
	// +kubebuilder:default=true
	// +optional
	AutoStart *bool `json:"autoStart,omitempty"`

	// OnNewRevision controls what happens when an even newer revision appears
	// while a rollout is in flight: Restart (abandon and retarget the newest,
	// default) or Queue (finish the current rollout first).
	// +kubebuilder:validation:Enum=Restart;Queue
	// +kubebuilder:default=Restart
	// +optional
	OnNewRevision NewRevisionPolicy `json:"onNewRevision,omitempty"`

	Canary    CanarySpec    `json:"canary"`
	Soak      SoakSpec      `json:"soak"`
	Promotion PromotionSpec `json:"promotion"`
	Waves     WaveSpec      `json:"waves"`

	// Triggers is the manual control surface: start, approve, resume, abort.
	// Entries are keyed by composition so one config can drive several
	// independent revision streams.
	// +optional
	Triggers []RolloutTrigger `json:"triggers,omitempty"`
}

type CompositionTarget struct {
	// Name of the cluster-scoped Composition.
	Name string `json:"name"`

	// XRSelector optionally narrows the governed XRs further (beyond the
	// composition ref), e.g. to specific namespaces or labels.
	// +optional
	XRSelector *XRSelector `json:"xrSelector,omitempty"`
}

type XRSelector struct {
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// CanarySpec selects the subset upgraded first. Exactly one strategy is set
// (enforced by CEL validation).
type CanarySpec struct {
	// +kubebuilder:validation:Enum=LabelSelector;Count;Percent
	Strategy CanaryStrategy `json:"strategy"`

	// LabelSelector picks XRs carrying specific labels (e.g. team-designated
	// guinea pigs). Used when Strategy=LabelSelector.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Count is a fixed number of XRs. Used when Strategy=Count.
	// +optional
	Count *int32 `json:"count,omitempty"`

	// Percent of the governed fleet, rounded up, minimum 1.
	// Used when Strategy=Percent.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	Percent *int32 `json:"percent,omitempty"`

	// PreferredNamespaces biases Count/Percent selection: canaries are drawn
	// from these namespaces first, in list order, using the deterministic hash
	// ordering within each namespace, before falling back to the rest of the
	// fleet. Lets a team e.g. canary in dev namespaces before prod ones within
	// the same cluster. Ignored for Strategy=LabelSelector.
	// +optional
	PreferredNamespaces []string `json:"preferredNamespaces,omitempty"`
}

type SoakSpec struct {
	// Duration the canary set must hold Ready=True and Synced=True.
	// +kubebuilder:default="30m"
	Duration metav1.Duration `json:"duration"`

	// MaxUnhealthy is the number of canary XRs allowed to go unhealthy during
	// the soak before the rollout pauses. Default 0 (any failure pauses).
	// +kubebuilder:default=0
	// +optional
	MaxUnhealthy int32 `json:"maxUnhealthy,omitempty"`

	// StabilisationTimeout bounds how long the controller waits for canaries
	// to first become healthy before declaring the canary failed.
	// +kubebuilder:default="30m"
	// +optional
	StabilisationTimeout *metav1.Duration `json:"stabilisationTimeout,omitempty"`
}

// PromotionSpec maps environment names to post-soak behaviour. The controller
// knows its own environment from a startup flag (see below); the same
// CompositionRollout manifest can therefore be GitOps-synced unchanged to
// every cluster.
type PromotionSpec struct {
	// Environments maps an environment name (dev, test, acceptance, prod, …)
	// to a promotion policy. The controller looks up its own environment; if
	// absent, DefaultPolicy applies.
	// +optional
	Environments map[string]PromotionPolicy `json:"environments,omitempty"`

	// +kubebuilder:validation:Enum=AutoPromote;ManualApproval
	// +kubebuilder:default=ManualApproval
	DefaultPolicy PromotionPolicy `json:"defaultPolicy"`
}

type WaveSpec struct {
	// BatchSize is the number (int) or percentage (string, e.g. "10%") of
	// remaining XRs updated per wave.
	// +kubebuilder:default="10%"
	BatchSize intstr.IntOrString `json:"batchSize"`

	// MaxConcurrentUpdates caps in-flight XR patches across a wave
	// (API-server write throttling). Default 20.
	// +kubebuilder:default=20
	// +optional
	MaxConcurrentUpdates int32 `json:"maxConcurrentUpdates,omitempty"`

	// WaveSoak is the health-hold period between waves. Defaults to a short
	// interval (not the full canary soak).
	// +kubebuilder:default="5m"
	// +optional
	WaveSoak metav1.Duration `json:"waveSoak,omitempty"`

	// MaxUnhealthy per wave before pausing. Default 0.
	// +kubebuilder:default=0
	// +optional
	MaxUnhealthy int32 `json:"maxUnhealthy,omitempty"`
}

// RolloutTrigger is the manual control surface. Written by humans (or
// pipelines) via kubectl/GitOps; consumed and acknowledged by the controller
// in status. See "approval mechanism" below for the rationale.
type RolloutTrigger struct {
	// Composition this trigger applies to.
	Composition string `json:"composition"`

	// +kubebuilder:validation:Enum=Start;Approve;Resume;Abort
	Action TriggerAction `json:"action"`

	// Revision the action applies to (a CompositionRevision name). Required
	// for Approve/Resume/Abort so a stale trigger can never act on a newer
	// revision than the human reviewed. Optional for Start (defaults to the
	// latest revision).
	// +optional
	Revision string `json:"revision,omitempty"`

	// Actor is free-text identification for audit (also captured via events
	// and managedFields).
	// +optional
	Actor string `json:"actor,omitempty"`
}
```

```go
type CompositionRolloutStatus struct {
	// ObservedGeneration of the spec last acted upon.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions: Progressing, Paused, AwaitingApproval, Succeeded (see §3).
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Rollouts tracks one entry per governed composition (each composition has
	// its own revision stream and independent state machine).
	Rollouts []CompositionRolloutState `json:"rollouts,omitempty"`
}

type CompositionRolloutState struct {
	Composition string `json:"composition"`

	// Phase: Idle, Canarying, Soaking, AwaitingApproval, Promoting, Paused,
	// Completed, Aborted.
	Phase RolloutPhase `json:"phase"`

	// TargetRevision is the CompositionRevision being rolled out.
	TargetRevision RevisionInfo `json:"targetRevision,omitempty"`

	// Fleet accounting (Manual-policy XRs only).
	TotalXRs      int32 `json:"totalXRs"`
	UpdatedXRs    int32 `json:"updatedXRs"`
	HealthyXRs    int32 `json:"healthyXRs"`
	UnmanagedXRs  int32 `json:"unmanagedXRs"`  // policy flipped to Automatic etc.

	// Canary membership, frozen at canary start for stability.
	Canary CanaryStatus `json:"canary,omitempty"`

	// Wave progress during Promoting.
	Wave WaveStatus `json:"wave,omitempty"`

	// PauseReason is set when Phase=Paused (machine-readable reason plus
	// human message).
	// +optional
	PauseReason *PauseReason `json:"pauseReason,omitempty"`

	// AcknowledgedTriggers records which spec.triggers entries have been
	// consumed, so a trigger acts exactly once.
	// +optional
	AcknowledgedTriggers []AcknowledgedTrigger `json:"acknowledgedTriggers,omitempty"`

	StartedAt   *metav1.Time `json:"startedAt,omitempty"`
	SoakEndsAt  *metav1.Time `json:"soakEndsAt,omitempty"`
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

type RevisionInfo struct {
	Name     string `json:"name"`
	Revision int64  `json:"revision"` // CompositionRevision spec.revision number
}

type CanaryStatus struct {
	// Selected lists the frozen canary members (namespace/name). Canary sets
	// are small by design, so the list is bounded.
	Selected  []XRRef `json:"selected,omitempty"`
	Healthy   int32   `json:"healthy"`
	Unhealthy int32   `json:"unhealthy"`
}

type WaveStatus struct {
	Current     int32        `json:"current"`
	Total       int32        `json:"total"`
	WaveEndsAt  *metav1.Time `json:"waveEndsAt,omitempty"`
}
```

### YAML example

```yaml
apiVersion: rollouts.platform.example.org/v1alpha1
kind: CompositionRollout
metadata:
  name: team-payments
spec:
  compositions:
    - name: platformdb.example.org
    - name: platformcache.example.org
      xrSelector:
        namespaces: [payments-dev, payments-prod]
  autoStart: true
  onNewRevision: Restart
  canary:
    strategy: Percent
    percent: 5
    preferredNamespaces: [payments-dev]
  soak:
    duration: 1h
    maxUnhealthy: 0
    stabilisationTimeout: 30m
  promotion:
    defaultPolicy: ManualApproval
    environments:
      dev:        AutoPromote
      test:       AutoPromote
      acceptance: ManualApproval
      prod:       ManualApproval
  waves:
    batchSize: "10%"
    maxConcurrentUpdates: 25
    waveSoak: 10m
  triggers:
    - composition: platformdb.example.org
      action: Approve
      revision: platformdb.example.org-9f8a2c1
      actor: ricardo@example.org
status:
  conditions:
    - type: AwaitingApproval
      status: "False"
      reason: Approved
    - type: Progressing
      status: "True"
      reason: Promoting
  rollouts:
    - composition: platformdb.example.org
      phase: Promoting
      targetRevision: { name: platformdb.example.org-9f8a2c1, revision: 7 }
      totalXRs: 412
      updatedXRs: 96
      healthyXRs: 96
      canary:
        selected:
          - { namespace: payments-dev, name: orders-db }
          # …frozen at canary start
        healthy: 21
        unhealthy: 0
      wave: { current: 2, total: 9, waveEndsAt: "2026-11-11T09:30:00Z" }
      acknowledgedTriggers:
        - { action: Approve, revision: platformdb.example.org-9f8a2c1, actor: ricardo@example.org }
```

### Approval mechanism — chosen design and rationale

**Chosen: the `spec.triggers` list** (above), rather than an annotation:

- **RBAC and admission control.** Spec writes can be restricted via a `ValidatingAdmissionPolicy` (e.g. only members of a release-manager group may add `action: Approve` entries). Annotations are writable by anyone with generic update rights and are invisible to most policy tooling.
- **GitOps-friendly and auditable.** An approval is a reviewable Git commit; `managedFields` plus controller events record who/when.
- **Revision-bound.** `revision` is mandatory for Approve/Resume/Abort, so an approval given for revision 7 can never accidentally promote revision 8 (TOCTOU protection).
- **Acknowledged, not deleted.** The controller never mutates spec (GitOps would fight it); it records consumption in `status.rollouts[].acknowledgedTriggers`. A trigger already acknowledged for the same (composition, action, revision) is a no-op. Stale triggers (for superseded revisions) are acknowledged with `outcome: Stale` and surfaced as an event.

**Environment configuration:** the controller binary takes `--environment=dev|test|acceptance|prod` (a per-cluster deployment flag, settable via Helm value/kustomize overlay). The CRD maps environment → policy, so identical manifests are synced everywhere and each cluster behaves per its own flag. A missing key falls back to `defaultPolicy` (which defaults to `ManualApproval` — fail safe).

---

## 3. Rollout state machine

Each governed composition in a `CompositionRollout` runs an independent state machine:

```
                 new CompositionRevision (autoStart) or Start trigger
                                    │
  Idle ─────────────────────────────▶ Canarying ──(all canaries patched)──▶ Soaking
   ▲                                     │                                    │
   │                                     │ failure                           │ failure / stabilisation timeout
   │                                     ▼                                    ▼
   │                                   Paused ◀───────────────────────────  Paused
   │                                     │  Resume ──▶ back to prior phase
   │                                     │  Abort  ──▶ Aborted ──▶ Idle
   │                                     │
   │          soak passes                ▼
   │   ┌── AutoPromote ──────────▶ Promoting ──(waves done, healthy)──▶ Completed ──▶ Idle
   │   │                              │ ▲                                   │
  Soaking ── ManualApproval ──▶ AwaitingApproval                            │
                                      │        Approve trigger ─────────────┘ (enters Promoting)
                                      │ failure during waves ──▶ Paused
```

| Phase | Meaning | Exit |
|---|---|---|
| `Idle` | No rollout in flight; fleet converged on current revision. | New revision + `autoStart`, or `Start` trigger. |
| `Canarying` | Canary set frozen in status; refs being patched. | All canaries patched → `Soaking`. Patch errors beyond retry budget → `Paused`. |
| `Soaking` | Waiting `soak.duration` with canaries healthy. | Healthy for full window → `AwaitingApproval` or `Promoting` (per env policy). Unhealthy > `maxUnhealthy`, or stabilisation timeout → `Paused`. |
| `AwaitingApproval` | Gate for `ManualApproval` environments. | `Approve` trigger (revision-matched) → `Promoting`. `Abort` → `Aborted`. |
| `Promoting` | Remainder rolls in waves with `waveSoak` holds. | All waves done and healthy → `Completed`. Wave failure → `Paused`. |
| `Paused` | Human attention needed; no further writes. | `Resume` (returns to the phase it paused from) or `Abort`. |
| `Completed` | All governed XRs on target revision. | Transitions to `Idle` (terminal record kept in status until next rollout). |
| `Aborted` | Rollout abandoned; **no rollback** — already-updated XRs stay on the new revision. | Returns to `Idle`; the aborted revision is recorded so `autoStart` does not immediately re-trigger it. |

**`onNewRevision: Restart`** (default): if a newer revision lands mid-rollout, the current rollout is marked superseded (event emitted) and the machine restarts at `Canarying` targeting the newest revision — XRs already moved to the intermediate revision are simply part of the pending set again. `Queue` finishes the current rollout first.

### Representation in status conditions

Top-level `metav1.Condition`s on the `CompositionRollout` (aggregated across compositions; per-composition detail lives in `status.rollouts[]`):

| Condition | True when | Reasons |
|---|---|---|
| `Progressing` | Any composition in Canarying/Soaking/Promoting. | `Canarying`, `Soaking`, `Promoting`, `Superseded` |
| `AwaitingApproval` | Any composition gated. | `SoakPassed` |
| `Paused` | Any composition paused. | `CanaryUnhealthy`, `WaveUnhealthy`, `StabilisationTimeout`, `PatchFailed`, `CanarySetEmpty`, `Suspended` |
| `Succeeded` | All compositions Idle/Completed on their latest revisions. | `AllCompleted`, or `False` with the blocking reason |

Phases are also printed via `+kubebuilder:printcolumn` for `kubectl get compositionrollouts` ergonomics.

---

## 4. Reconcile architecture

### Controllers and watches

A single manager with **one primary reconciler** keyed on `CompositionRollout`. There is deliberately **no per-XR reconciler**: one reconcile pass per rollout evaluates the whole fleet from cache. Watches:

1. **`CompositionRollout`** — primary.
2. **`CompositionRevision`** (cluster-scoped, typed client from `crossplane/crossplane` apis) — mapped to rollouts via an **index on `spec.compositions[].name`** matching the revision's `crossplane.io/composition-name` label. Predicate: enqueue only on create (revisions are immutable).
3. **`Composition`** — only to resolve `spec.compositeTypeRef` → the XR GroupVersionKind(s) to watch, and to detect targets whose type ref changed (out of scope → pause with reason).
4. **XRs, via dynamic unstructured watches.** XR types are arbitrary, so on each reconcile the controller computes the set of needed GVKs from the governed compositions' `compositeTypeRef` and lazily registers a watch per GVK:
   - `source.Kind(mgr.GetCache(), &unstructured.Unstructured{ /* GVK set */ }, handler)` added via `controller.Watch`. Registered GVKs are tracked in a mutex-guarded map to make registration idempotent.
   - When a composition is removed from every config, the informer is released with `cache.RemoveInformer` (controller-runtime ≥ v0.17) so memory is reclaimed.
   - A **cache transform function** strips `metadata.managedFields` and any large non-needed fields from cached XRs; the controller needs only metadata, `spec.crossplane.*`, and `status.conditions`. (Metadata-only watches are insufficient because policy, refs, and conditions are needed.)
   - The XR→rollout event handler maps an XR to its governing rollout via the composition-name index (below) and uses **predicates** so that only relevant changes enqueue: changes to `spec.crossplane.compositionRevisionRef`, `spec.crossplane.compositionUpdatePolicy`, or the `Ready`/`Synced` conditions; plus create/delete. Everything else (status noise on unrelated conditions, label churn) is filtered out.

### Field indexes

| Index | On | Key | Used for |
|---|---|---|---|
| `xr.compositionRef` | each watched XR GVK (unstructured) | `spec.crossplane.compositionRef.name` | O(1) listing of a composition's fleet from cache |
| `rollout.compositions` | `CompositionRollout` | each `spec.compositions[].name` | mapping revision/XR events to the owning rollout |

Fleet listing is always `cache.List` with the index selector — never a live API list — so reconciles are cheap at any fleet size.

### Requeue strategy

Event-driven first; timers only where time itself is the trigger:

- `RequeueAfter(soakEndsAt - now)` while Soaking, and `RequeueAfter(waveEndsAt - now)` between waves. Timestamps are persisted in status, so timers survive restarts and leader failover.
- Condition flips on XRs arrive as watch events (no polling for health).
- Standard exponential backoff on transient API errors; a bounded retry budget per phase before escalating to `Paused` with `PatchFailed`.

### Stable canary selection

Requirement: the same XRs are chosen across reconciles, controller restarts, and leader changes.

1. **Determinism:** for `Count`/`Percent`, compute `fnv64a(xr.UID)` per governed XR, sort by `(hash, namespace/name)` (name as tiebreak), and take the first N. UID-based hashing is uniform, independent of creation order, and immune to alphabetical bias in naming conventions. When `canary.preferredNamespaces` is set, candidates are bucketed — the listed namespaces first, in list order, then everything else — and the hash ordering is applied within each bucket, so selection remains fully deterministic while favouring the designated namespaces. For `LabelSelector`, the selector result *is* the canary set (validated non-empty, else `Paused`/`CanarySetEmpty`).
2. **Freezing:** determinism alone is not enough — the fleet mutates mid-rollout (creates/deletes would reshuffle a pure hash-top-N). So the selected set is **persisted in `status.rollouts[].canary.selected` at the `Canarying` transition** and never recomputed for that rollout. Canary sets are small by design, so the status list is bounded; a validation cap (e.g. 500 members) guards against pathological configs, falling back to counts plus a hash of the membership list.
3. A deleted canary member shrinks the set (with an event); if the set becomes empty mid-soak, the rollout pauses with `CanarySetEmpty`.

### Reconcile pass (per rollout, per composition)

1. Resolve latest `CompositionRevision` (highest `spec.revision` for the composition); compare with `status.targetRevision` and the fleet's current refs; decide phase transitions per §3 and pending triggers.
2. List the fleet from cache via the composition-ref index; partition into *managed* (policy `Manual`, matching `xrSelector`), *unmanaged*, *updated*, *pending*, *unhealthy*.
3. Execute the phase's action (patch canaries / check soak / patch next wave) under `waves.maxConcurrentUpdates` using a bounded worker pool.
4. Write status with optimistic concurrency (status subresource), emit events, set requeue timer if needed.

---

## 5. Interaction with Crossplane

### Exactly what the controller writes

One field, on governed XRs only:

```yaml
spec:
  crossplane:
    compositionRevisionRef:
      name: <target CompositionRevision name>
```

Applied with **server-side apply**, field manager `xr-rollout-controller`, patching only that field. The controller never touches `compositionUpdatePolicy`, `compositionRef`, `compositionRevisionSelector`, labels, or any schema field of the XR.

### Ownership and conflicts

- **Crossplane's composite controller** writes `compositionRevisionRef` itself for `Automatic` XRs (it updates the ref to the latest revision on reconcile — see Crossplane's revision fetcher behaviour). The rollout controller therefore governs **only `Manual` XRs**; there is no write contention with Crossplane by construction. `Automatic` XRs under a governed composition are counted in `status.unmanagedXRs` for visibility and otherwise left alone — the two policies coexist: Automatic XRs jump to new revisions immediately on their own, Manual ones move only when the rollout promotes them.
- **GitOps engines (Argo CD / Flux):** if the XR manifest in Git pins `compositionRevisionRef`, the sync engine will fight the rollout controller. Documented prerequisite: Git must either omit the ref (Crossplane initialises it at creation) or the engine must be told to ignore it (Argo `ignoreDifferences` on `/spec/crossplane/compositionRevisionRef`; Flux drift exclusion). SSA conflicts (another field manager owning the field) are **not** force-overridden by default; a conflict pauses the rollout with `PatchFailed` naming the conflicting manager, because silently stealing the field would create a permanent tug-of-war. A `--force-ownership` escape hatch flag exists for clusters that have verified their GitOps config.
- **Humans with kubectl** editing refs mid-rollout are detected on the next reconcile (the XR's ref ≠ expected) and simply re-counted; during an active rollout the controller re-asserts the target ref for members of already-promoted sets.

### Drift scenarios

| Scenario | Handling |
|---|---|
| **Policy flipped `Manual` → `Automatic` mid-rollout** | XR leaves the governed set on the next event (predicate catches the policy change). It is moved to `unmanagedXRs`, excluded from wave/canary accounting (if it was a canary, the set shrinks), and an event records it. Crossplane will upgrade it immediately on its own. |
| **XR deleted mid-rollout** | Tolerated: `NotFound` on patch is success; deletion events shrink the pending/canary sets. Fleet totals are recomputed each reconcile from cache, so progress percentages stay truthful. |
| **New XR created mid-rollout** | Crossplane pins new XRs to the **latest** revision at creation regardless of policy, so new instances usually arrive already on (or past) the target and are counted as updated. An XR created pinned to an older ref (e.g. ref specified in Git) joins the pending remainder and rolls in a later wave. |
| **Newer revision appears mid-rollout** | Per `onNewRevision` (§3): restart targeting the newest, or queue. |
| **Composition's `compositeTypeRef` changes** (schema move) | Out of scope by definition; rollout pauses with a dedicated reason so humans decide. |
| **Target revision deleted** (Crossplane GCs old revisions) | If the *target* disappears, pause with `TargetRevisionGone`; if an *old* source revision disappears, irrelevant — the controller only writes forward. |

---

## 6. Failure and pause semantics; resume and abort

**Failure definition:** during `Soaking` (or a wave's `waveSoak`), a governed updated XR whose `Ready` or `Synced` condition is not `True` counts as unhealthy. Health over the soak window is implemented as: *condition currently `True` AND `lastTransitionTime` ≤ soak start* — any mid-soak flip to `False` is caught by the watch event and fails the XR even if it recovers (flapping is a failure). Exceeding `maxUnhealthy`, or canaries not reaching health within `stabilisationTimeout`, pauses the rollout.

**On pause:**

- No further `compositionRevisionRef` writes for that composition. Already-updated XRs are **left on the new revision** (no rollback — reverting a composition can itself be destructive, so that call belongs to a human).
- `Paused` condition set with a machine-readable reason and a message naming the unhealthy XRs (capped list), `status.rollouts[].pauseReason` populated, a `Warning` event emitted (e.g. `RolloutPaused: canary payments-prod/orders-db Synced=False: cannot render composed resources…`), and the `paused` metric set (§7) for alerting.

**Resume flow:** a human diagnoses (often fixing the composition — which creates a *new* revision and, with `onNewRevision: Restart`, naturally retargets the rollout) or fixes environmental causes. To retry the same revision:

```yaml
spec:
  triggers:
    - { composition: platformdb.example.org, action: Resume,
        revision: platformdb.example.org-9f8a2c1, actor: ricardo@example.org }
```

Resume re-enters the phase that paused (`Soaking` restarts its window; `Promoting` re-runs the failed wave). The revision binding makes a stale resume harmless.

**Abort flow:** `action: Abort` (same shape) transitions to `Aborted` → `Idle`. No writes are undone — the mixed fleet (canaries on the new revision, remainder on the old) is left as-is by design; status records the aborted revision with how many XRs ended up on it, and `autoStart` will not re-trigger an aborted revision (only a newer one). **Retry is always available:** a `Start` trigger naming the aborted revision runs the full rollout again from `Canarying` (the frozen canary set is recomputed). `Start` accepts any explicit revision of the composition, so a team can also deliberately re-converge the fleet on the *older* revision the same way.

`spec.suspend: true` is the global brake: it pauses everything with reason `Suspended` and ignores triggers until lifted.

---

## 7. Observability

**Conditions** — §3. **Events** (on the `CompositionRollout`; the interesting per-XR ones also on the XR itself):

`RolloutStarted`, `CanarySelected` (with membership summary), `XRUpdated`, `SoakStarted`, `SoakPassed`, `AwaitingApproval`, `Approved` (with actor), `WaveStarted`/`WaveCompleted`, `RolloutPaused` (Warning), `RolloutResumed`, `RolloutAborted`, `RolloutCompleted`, `RolloutSuperseded`, `TriggerIgnored` (Warning, stale/duplicate trigger).

**Prometheus metrics** (via controller-runtime's registry; labels kept to `rollout`, `composition`, and where noted `phase`/`reason` — never per-XR labels, to bound cardinality):

| Metric | Type | Notes |
|---|---|---|
| `xr_rollout_phase` | gauge | 1 for the current phase, labelled `phase`; enables `AwaitingApproval`/`Paused` alerts |
| `xr_rollout_xrs{state=managed\|updated\|healthy\|unmanaged}` | gauge | fleet accounting |
| `xr_rollout_canary_unhealthy` | gauge | drives canary-failure alerts |
| `xr_rollout_updates_total` / `xr_rollout_update_errors_total` | counter | ref patches attempted/failed |
| `xr_rollout_pauses_total{reason}` | counter | |
| `xr_rollout_duration_seconds` | histogram | started → completed, labelled `composition` |
| `xr_rollout_wave_duration_seconds` | histogram | |

Plus standard controller-runtime workqueue/reconcile metrics. Suggested alerts: paused > 15 min; awaiting approval > SLA; update error rate; rollout duration SLO.

Clusters host many compositions (confirmed), so series count is linear in composition count — roughly a dozen series per composition, which Prometheus absorbs comfortably at hundreds of compositions. Per-XR labels remain strictly forbidden. A `--metrics-aggregate-only` flag drops the `composition` label for clusters that outgrow the budget.

**Logging:** structured (zap), one line per transition and per wave, with `rollout`, `composition`, `revision`, counts. Per-XR patches logged at debug level only.

---

## 8. RBAC and security

**Controller permissions:**

- `apiextensions.crossplane.io`: `compositionrevisions`, `compositions` — get/list/watch.
- `rollouts.platform.example.org`: `compositionrollouts` get/list/watch; `compositionrollouts/status` update/patch; events create/patch; leases (leader election).
- **XR types** — get/list/watch/patch. XR groups are arbitrary, which is the hard part. Options, in order of preference:
  1. **Aggregated ClusterRole:** the controller ships a ClusterRole with an `aggregationRule` selecting label `rollouts.platform.example.org/aggregate-to-rollout-controller: "true"`; the platform team (or a tiny optional helper controller watching XRDs) creates one small labelled ClusterRole per XR group. Explicit, auditable, no wildcards.
  2. Piggyback on the **Crossplane RBAC manager's** aggregation labels where it is deployed (it already maintains per-XRD ClusterRoles).
  3. Wildcard `*` on a configured list of API groups — simplest, acceptable in single-tenant clusters; rejected as the default.

**Security considerations:**

- The controller writes exactly one field on XRs and nothing on Compositions/revisions; it cannot alter what a revision *does*, only *when* a fleet adopts it.
- **Approval integrity:** `spec.triggers` is the privileged surface. A shipped `ValidatingAdmissionPolicy` example restricts `Approve`/`Resume` entries to designated groups, and (optionally) requires `actor` to match the request's authenticated user. Events + `managedFields` give the audit trail.
- Multi-team isolation: the CRD is cluster-scoped, and **admission rejects overlap** — a shipped `ValidatingAdmissionPolicy` denies creating or updating a `CompositionRollout` that claims a composition already claimed by another one (decided; see §11). The controller still detects overlap at reconcile time (admission can be bypassed or disabled) and pauses both configs with a clear reason as defence in depth.
- Least-privilege stance: no secrets access, no pod exec, read-only on everything except XR revision refs and its own status.

---

## 9. Scale considerations

Assumption: thousands of XRs per composition, and **many compositions per cluster** (confirmed by the team), spread across multiple `CompositionRollout` configs.

- **No per-instance polling.** All fleet reads come from the shared informer cache via field indexes; health arrives as watch events; timers exist only for soak/wave boundaries.
- **Memory:** unstructured XR caches are the main cost. The cache transform (strip managed fields, prune to needed subtrees) typically cuts per-object size dramatically; informers for GVKs no longer governed are removed. Watches are started per *governed* GVK only, not for all XRDs in the cluster.
- **Write throughput:** waves bound blast radius (`batchSize`) while `maxConcurrentUpdates` bounds API-server pressure within a wave; patches go through a worker pool honouring client QPS/burst (tuned flags, e.g. QPS 50/burst 100). A 10 000-XR wave at 25 concurrent ≈ minutes, not hours, without starving other API clients.
- **Reconcile cost:** one pass is O(fleet) over cached, pruned objects with an indexed list — no API round-trips proportional to fleet size. Status stores aggregates plus the (small) canary membership; never the full fleet list.
- **Event storms:** predicates drop XR events that don't affect policy/ref/health; during `Idle`, condition-flip events for a composition with no active rollout are additionally rate-limited via a `TypedRateLimiter` so a flapping fleet can't hot-loop the reconciler.
- **Leader election** on; a single active reconciler is sufficient because per-rollout work is cheap and wave patching is internally parallel. Reconciles for distinct `CompositionRollout`s run concurrently (`MaxConcurrentReconciles` ~4).
- Restart safety: all phase state (frozen canary set, soak/wave deadlines, acknowledged triggers) lives in status, so failover resumes mid-soak without restarting windows.

---

## 10. Kubebuilder project layout and milestones

```
xr-rollout-controller/
├── cmd/main.go                          # flags: --environment, --force-ownership, QPS/burst
├── api/v1alpha1/
│   ├── compositionrollout_types.go      # types from §2 + CEL validation markers
│   └── zz_generated.deepcopy.go
├── internal/
│   ├── controller/
│   │   ├── rollout_controller.go        # primary reconciler, phase dispatch
│   │   ├── watches.go                   # dynamic XR watch registry (add/remove informers)
│   │   └── indexes.go                   # field indexes
│   ├── rollout/
│   │   ├── statemachine.go              # pure transition logic (table-driven, unit-testable)
│   │   ├── canary.go                    # hash-based + label-based selection, freezing
│   │   ├── health.go                    # Ready/Synced soak evaluation
│   │   ├── waves.go                     # batch computation, bounded patch pool
│   │   └── triggers.go                  # trigger matching/acknowledgement
│   ├── xp/                              # Crossplane accessors for unstructured XRs
│   │   └── xr.go                        # Get/SetRevisionRef, GetPolicy, conditions
│   └── metrics/metrics.go
├── config/                              # kubebuilder scaffolding: crd/, rbac/ (incl.
│   │                                    # aggregation ClusterRole), manager/, samples/
│   └── policy/approval-vap.yaml         # example ValidatingAdmissionPolicy
└── test/e2e/                            # envtest + kind-based suite with crossplane
```

Dependencies: `sigs.k8s.io/controller-runtime`, `github.com/crossplane/crossplane/apis` (typed `CompositionRevision`/`Composition`), `k8s.io/apimachinery` unstructured helpers.

### Milestones

1. **M1 — Skeleton and fleet visibility.** CRD scaffold, revision/composition watches, dynamic XR watch registry, indexes, status fleet accounting. Exit: accurate `totalXRs`/revision reporting on a kind cluster with Crossplane v2.
2. **M2 — Canary engine.** Selection strategies, freezing, SSA ref patching, conflict detection. Exit: canary pinning works end-to-end; selection proven stable across restarts (envtest).
3. **M3 — State machine with soak and pause.** Health evaluation, soak timers, pause on failure, `Paused` conditions/events. Exit: unhealthy canary pauses; no writes occur while paused.
4. **M4 — Triggers and promotion gate.** Trigger consumption/acknowledgement, `--environment` + policy mapping, `AwaitingApproval` flow, resume/abort. Exit: full dev-auto vs prod-approve paths in e2e.
5. **M5 — Waves and scale hardening.** Batch computation, concurrency limits, wave soaks, cache transforms, informer removal, predicates/rate limiting; load test at ~10k synthetic XRs.
6. **M6 — Production polish.** Metrics/alerts, admission policy example, aggregated RBAC helper, drift-scenario e2e matrix (§5 table), docs and runbooks.

---

## 11. Decisions and remaining open questions

### Resolved with the team (2026-11-11)

1. **Abort aftermath:** leave the mixed fleet as-is — no `revertCanariesOnAbort`. Retry is explicitly supported: a `Start` trigger re-runs the full rollout for the aborted (or any explicit) revision (§6).
2. **Pre-rollout validation:** no render/diff gate in the controller; `crossplane composition render` checks belong in CI on the Composition repo.
3. **Overlap policy:** admission **rejects** a `CompositionRollout` claiming a composition already claimed by another; controller-side pause-on-overlap kept only as defence in depth (§8).
4. **Canary ordering:** selection provides for namespaces — `canary.preferredNamespaces` biases Count/Percent selection to draw canaries from designated namespaces first, deterministically (§2, §4).
5. **Adoption mode:** none. The controller never writes `compositionUpdatePolicy`; bringing XRs under governance is done by XR authors or the XRD's `defaultCompositionUpdatePolicy: Manual`.
6. **Environment source:** every cluster is one environment — the per-cluster `--environment` deployment flag stands; no per-namespace environments.
7. **Metrics cardinality:** clusters host many compositions; per-composition labels retained (bounded, no per-XR labels), with a `--metrics-aggregate-only` escape hatch (§7).

### Still open

1. **Approval UX:** is `kubectl`-applied `spec.triggers` (optionally via Git PR) sufficient, or do we want a small CLI/plugin (`kubectl rollout-xp approve …`) and/or chat-ops integration in v1?
2. **Revision GC interplay:** Crossplane keeps a limited revision history; do fleets ever sit pinned long enough that the *current* (old) revision could be GC'd, and do we need to alert on that?

---

### References

- [Composition Revisions — Crossplane v2.4 docs](https://docs.crossplane.io/latest/composition/composition-revisions/) (revision creation, `compositionUpdatePolicy`, `compositionRevisionRef`, ref set at creation time, revision numbering, label `crossplane.io/composition-name`)
- [Composite Resource Definitions — Crossplane v2.4 docs](https://docs.crossplane.io/latest/composition/composite-resource-definitions/) (`defaultCompositionUpdatePolicy`)
- [Crossplane API reference](https://docs.crossplane.io/latest/api/) (`CompositionRevision` schema, default policies)
- Crossplane source: `internal/controller/apiextensions/composite/api.go` revision fetcher (Automatic-policy XRs have their ref written by Crossplane itself; Manual XRs are fetched by pinned ref) — [github.com/crossplane/crossplane](https://github.com/crossplane/crossplane/blob/main/internal/controller/apiextensions/composite/api.go)
