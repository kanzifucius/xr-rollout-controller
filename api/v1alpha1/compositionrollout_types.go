/*
Copyright 2026 Ricardo Pinto.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Condition and phase constants for CompositionRollout status.
const (
	ConditionProgressing      = "Progressing"
	ConditionAwaitingApproval = "AwaitingApproval"
	ConditionPaused           = "Paused"
	ConditionSucceeded        = "Succeeded"

	PhaseIdle             RolloutPhase = "Idle"
	PhaseCanarying        RolloutPhase = "Canarying"
	PhaseSoaking          RolloutPhase = "Soaking"
	PhaseAwaitingApproval RolloutPhase = "AwaitingApproval"
	PhasePromoting        RolloutPhase = "Promoting"
	PhasePaused           RolloutPhase = "Paused"
	PhaseCompleted        RolloutPhase = "Completed"
	PhaseAborted          RolloutPhase = "Aborted"

	DryRunStageCanary DryRunStage = "Canary"
	DryRunStageWave   DryRunStage = "Wave"

	CanaryStrategyLabelSelector CanaryStrategy = "LabelSelector"
	CanaryStrategyCount         CanaryStrategy = "Count"
	CanaryStrategyPercent       CanaryStrategy = "Percent"

	PromotionAutoPromote    PromotionPolicy = "AutoPromote"
	PromotionManualApproval PromotionPolicy = "ManualApproval"

	NewRevisionRestart NewRevisionPolicy = "Restart"
	NewRevisionQueue   NewRevisionPolicy = "Queue"

	TriggerStart   TriggerAction = "Start"
	TriggerApprove TriggerAction = "Approve"
	TriggerResume  TriggerAction = "Resume"
	TriggerAbort   TriggerAction = "Abort"

	PauseReasonCanaryUnhealthy      = "CanaryUnhealthy"
	PauseReasonWaveUnhealthy        = "WaveUnhealthy"
	PauseReasonStabilisationTimeout = "StabilisationTimeout"
	PauseReasonPatchFailed          = "PatchFailed"
	PauseReasonCanarySetEmpty       = "CanarySetEmpty"
	PauseReasonSuspended            = "Suspended"
	PauseReasonOverlap              = "CompositionOverlap"
	PauseReasonTargetRevisionGone   = "TargetRevisionGone"
	PauseReasonSchemaChange         = "SchemaChange"
)

// RolloutPhase is the phase of a single composition's rollout.
// +kubebuilder:validation:Enum=Idle;Canarying;Soaking;AwaitingApproval;Promoting;Paused;Completed;Aborted
type RolloutPhase string

// CanaryStrategy selects how the canary subset is chosen.
// +kubebuilder:validation:Enum=LabelSelector;Count;Percent
type CanaryStrategy string

// PromotionPolicy controls post-soak promotion behaviour.
// +kubebuilder:validation:Enum=AutoPromote;ManualApproval
type PromotionPolicy string

// NewRevisionPolicy controls mid-rollout appearance of a newer revision.
// +kubebuilder:validation:Enum=Restart;Queue
type NewRevisionPolicy string

// TriggerAction is a manual control action.
// +kubebuilder:validation:Enum=Start;Approve;Resume;Abort
type TriggerAction string

// DryRunStage is Canary or Wave.
type DryRunStage string

// CompositionRolloutSpec defines how revisions of a set of compositions are
// rolled out to the XRs that reference them.
type CompositionRolloutSpec struct {
	// Compositions governed by this rollout config. XRs are selected by
	// spec.crossplane.compositionRef.name matching one of these entries and
	// compositionUpdatePolicy == Manual.
	// +kubebuilder:validation:MinItems=1
	Compositions []CompositionTarget `json:"compositions"`

	// Cluster names the engaged cluster whose composite resources this object rolls.
	// Leave it empty when the object lives on that cluster.
	// With --mode=hub the object lives on this install and Cluster is required.
	// One hub object names one cluster.
	// +optional
	Cluster string `json:"cluster,omitempty"`

	// Suspend stops the controller acting on new revisions (and pauses any
	// in-flight rollout) without deleting state.
	// +optional
	Suspend bool `json:"suspend,omitempty"`

	// DryRun simulates this rollout. The controller records which Manual XRs
	// would move and does not write them. Default false.
	// +kubebuilder:default=false
	// +optional
	DryRun *bool `json:"dryRun,omitempty"`

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
	// The list is a map keyed by composition, action, and revision so a
	// field manager can own a single entry. The promoter's Start uses
	// field manager xr-rollout-promoter. Empty revision is a key: only one
	// Start with an empty revision is allowed per composition.
	// +listType=map
	// +listMapKey=composition
	// +listMapKey=action
	// +listMapKey=revision
	// +optional
	Triggers []RolloutTrigger `json:"triggers,omitempty"`
}

// CompositionTarget names a Composition and optional XR narrowing.
type CompositionTarget struct {
	// Name of the cluster-scoped Composition.
	Name string `json:"name"`

	// XRSelector optionally narrows the governed XRs further.
	// +optional
	XRSelector *XRSelector `json:"xrSelector,omitempty"`
}

// XRSelector narrows governed XRs by namespace and/or labels.
type XRSelector struct {
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// CanarySpec selects the subset upgraded first. Exactly one of LabelSelector,
// Count, or Percent should be set to match Strategy.
type CanarySpec struct {
	// +kubebuilder:validation:Enum=LabelSelector;Count;Percent
	Strategy CanaryStrategy `json:"strategy"`

	// LabelSelector picks XRs carrying specific labels.
	// Used when Strategy=LabelSelector.
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
	// from these namespaces first, in list order, using deterministic hash
	// ordering within each namespace, before falling back to the rest of the
	// fleet. Ignored for Strategy=LabelSelector.
	// +optional
	PreferredNamespaces []string `json:"preferredNamespaces,omitempty"`
}

// SoakSpec defines canary health soak requirements.
type SoakSpec struct {
	// Duration the canary set must hold Ready=True and Synced=True.
	// +kubebuilder:default="30m"
	Duration metav1.Duration `json:"duration"`

	// MaxUnhealthy is the number of canary XRs allowed to go unhealthy during
	// the soak before the rollout pauses. Default 0.
	// +kubebuilder:default=0
	// +optional
	MaxUnhealthy int32 `json:"maxUnhealthy,omitempty"`

	// StabilisationTimeout bounds how long the controller waits for canaries
	// to first become healthy before declaring the canary failed.
	// +kubebuilder:default="30m"
	// +optional
	StabilisationTimeout *metav1.Duration `json:"stabilisationTimeout,omitempty"`
}

// PromotionSpec maps environment names to post-soak behaviour.
type PromotionSpec struct {
	// Environments maps an environment name (dev, test, acceptance, prod, …)
	// to a promotion policy. The controller looks up its own environment.
	// +optional
	Environments map[string]PromotionPolicy `json:"environments,omitempty"`

	// +kubebuilder:validation:Enum=AutoPromote;ManualApproval
	// +kubebuilder:default=ManualApproval
	DefaultPolicy PromotionPolicy `json:"defaultPolicy"`
}

// WaveSpec configures batched promotion of the remainder.
type WaveSpec struct {
	// BatchSize is the number (int) or percentage (string, e.g. "10%") of
	// remaining XRs updated per wave.
	// +kubebuilder:default="10%"
	BatchSize intstr.IntOrString `json:"batchSize"`

	// MaxConcurrentUpdates caps in-flight XR patches across a wave.
	// +kubebuilder:default=20
	// +optional
	MaxConcurrentUpdates int32 `json:"maxConcurrentUpdates,omitempty"`

	// WaveSoak is the health-hold period between waves.
	// +kubebuilder:default="5m"
	// +optional
	WaveSoak metav1.Duration `json:"waveSoak,omitempty"`

	// MaxUnhealthy per wave before pausing. Default 0.
	// +kubebuilder:default=0
	// +optional
	MaxUnhealthy int32 `json:"maxUnhealthy,omitempty"`
}

// RolloutTrigger is the manual control surface.
type RolloutTrigger struct {
	// Composition this trigger applies to.
	Composition string `json:"composition"`

	// +kubebuilder:validation:Enum=Start;Approve;Resume;Abort
	Action TriggerAction `json:"action"`

	// Revision the action applies to (a CompositionRevision name). Required
	// for Approve/Resume/Abort. Optional for Start: empty means latest.
	// The empty default keeps this list-map key set when callers omit it.
	// +kubebuilder:default=""
	// +optional
	Revision string `json:"revision,omitempty"`

	// Actor is free-text identification for audit.
	// +optional
	Actor string `json:"actor,omitempty"`
}

// CompositionRolloutStatus is the observed state.
type CompositionRolloutStatus struct {
	// ObservedGeneration of the spec last acted upon.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions: Progressing, Paused, AwaitingApproval, Succeeded.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Rollouts tracks one entry per governed composition.
	// +listType=map
	// +listMapKey=composition
	// +optional
	Rollouts []CompositionRolloutState `json:"rollouts,omitempty"`
}

// CompositionRolloutState is the per-composition state machine snapshot.
type CompositionRolloutState struct {
	Composition string `json:"composition"`

	// Phase of this composition's rollout.
	Phase RolloutPhase `json:"phase"`

	// TargetRevision is the CompositionRevision being rolled out.
	// +optional
	TargetRevision RevisionInfo `json:"targetRevision,omitempty"`

	// SourceRevision is the revision the fleet was on when the rollout started.
	// +optional
	SourceRevision RevisionInfo `json:"sourceRevision,omitempty"`

	TotalXRs     int32 `json:"totalXRs"`
	UpdatedXRs   int32 `json:"updatedXRs"`
	HealthyXRs   int32 `json:"healthyXRs"`
	UnmanagedXRs int32 `json:"unmanagedXRs"`

	// Canary membership, frozen at canary start for stability.
	// +optional
	Canary CanaryStatus `json:"canary,omitempty"`

	// Wave progress during Promoting.
	// +optional
	Wave WaveStatus `json:"wave,omitempty"`

	// PauseReason is set when Phase=Paused.
	// +optional
	PauseReason *PauseReason `json:"pauseReason,omitempty"`

	// PhaseBeforePause records where Resume should return.
	// +optional
	PhaseBeforePause RolloutPhase `json:"phaseBeforePause,omitempty"`

	// AbortedRevision records a revision that was aborted so autoStart will
	// not immediately re-trigger it.
	// +optional
	AbortedRevision string `json:"abortedRevision,omitempty"`

	// AcknowledgedTriggers records consumed triggers.
	// +optional
	AcknowledgedTriggers []AcknowledgedTrigger `json:"acknowledgedTriggers,omitempty"`

	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// +optional
	SoakEndsAt *metav1.Time `json:"soakEndsAt,omitempty"`
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// DryRun is the simulation report. Set while spec.dryRun is true.
	// completedAt here is not the rollout completedAt.
	// +optional
	DryRun *DryRunStatus `json:"dryRun,omitempty"`
}

// DryRunStatus is the per-composition simulation report.
type DryRunStatus struct {
	// Active mirrors spec.dryRun for this composition.
	Active bool `json:"active"`
	// WouldUpdate lists Manual XRs whose compositionRevisionRef.name would
	// change, in patch order. Entries stay after pause and abort.
	// +optional
	WouldUpdate []DryRunUpdate `json:"wouldUpdate,omitempty"`
	// Promotion is the policy resolved for this controller's --environment.
	// +optional
	Promotion PromotionPolicy `json:"promotion,omitempty"`
	// CompletedAt is when the simulation finished. The rollout completedAt
	// stays empty.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// DryRunUpdate is one XR the controller would have patched.
type DryRunUpdate struct {
	// +optional
	Namespace string      `json:"namespace,omitempty"`
	Name      string      `json:"name"`
	Stage     DryRunStage `json:"stage"`
	// Wave is 1-based. Empty for a canary.
	// +optional
	Wave int32 `json:"wave,omitempty"`
	// Revision is the CompositionRevision name that would be written.
	Revision string `json:"revision"`
}

// RevisionInfo identifies a CompositionRevision.
type RevisionInfo struct {
	Name string `json:"name"`
	// Revision is CompositionRevision spec.revision number.
	Revision int64 `json:"revision"`
}

// CanaryStatus records frozen canary membership and health counts.
type CanaryStatus struct {
	// Selected lists the frozen canary members.
	// +optional
	Selected  []XRRef `json:"selected,omitempty"`
	Healthy   int32   `json:"healthy"`
	Unhealthy int32   `json:"unhealthy"`
}

// WaveStatus tracks wave progress.
type WaveStatus struct {
	Current int32 `json:"current"`
	Total   int32 `json:"total"`
	// +optional
	WaveEndsAt *metav1.Time `json:"waveEndsAt,omitempty"`
}

// XRRef identifies an XR by namespace/name (namespace empty for cluster-scoped).
type XRRef struct {
	// +optional
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// PauseReason explains why a rollout paused.
type PauseReason struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// AcknowledgedTrigger records a consumed trigger.
type AcknowledgedTrigger struct {
	Action   TriggerAction `json:"action"`
	Revision string        `json:"revision,omitempty"`
	// +optional
	Actor string `json:"actor,omitempty"`
	// Outcome is Applied or Stale.
	Outcome string `json:"outcome"`
	// +optional
	AcknowledgedAt *metav1.Time `json:"acknowledgedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=xrollout
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.rollouts[0].phase`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.cluster`
// +kubebuilder:printcolumn:name="Dry-run",type=boolean,JSONPath=`.spec.dryRun`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CompositionRollout is the Schema for the compositionrollouts API.
type CompositionRollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CompositionRolloutSpec   `json:"spec,omitempty"`
	Status CompositionRolloutStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CompositionRolloutList contains a list of CompositionRollout.
type CompositionRolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CompositionRollout `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CompositionRollout{}, &CompositionRolloutList{})
}

// AutoStartEnabled returns whether auto-start is enabled (default true).
func (s *CompositionRolloutSpec) AutoStartEnabled() bool {
	if s.AutoStart == nil {
		return true
	}
	return *s.AutoStart
}

// DryRunEnabled reports whether this object is simulating. Nil is false.
func (s *CompositionRolloutSpec) DryRunEnabled() bool {
	return s.DryRun != nil && *s.DryRun
}

// Simulation returns the would-update log. Nil when no report exists.
func (s *CompositionRolloutState) Simulation() []DryRunUpdate {
	if s.DryRun == nil {
		return nil
	}
	return s.DryRun.WouldUpdate
}

// PromotionPolicyFor returns the promotion policy for the given environment.
func (p *PromotionSpec) PromotionPolicyFor(environment string) PromotionPolicy {
	if p.Environments != nil {
		if pol, ok := p.Environments[environment]; ok {
			return pol
		}
	}
	if p.DefaultPolicy != "" {
		return p.DefaultPolicy
	}
	return PromotionManualApproval
}

// MaxConcurrent returns the concurrent update cap (default 20).
func (w *WaveSpec) MaxConcurrent() int32 {
	if w.MaxConcurrentUpdates <= 0 {
		return 20
	}
	return w.MaxConcurrentUpdates
}

func (r XRRef) String() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// XRKey builds a stable namespace/name key.
func XRKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}
