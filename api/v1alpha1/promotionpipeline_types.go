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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	PipelineReady       = "Ready"
	PipelineProgressing = "Progressing"
	PipelineBlocked     = "Blocked"

	// PromotionApprovalApprove holds a cluster until an Approve trigger
	// names that cluster and the revision being promoted.
	PromotionApprovalApprove PromotionApproval = "Approve"

	// PromotionTriggerApprove opens a cluster that sets approval: Approve.
	PromotionTriggerApprove PromotionTriggerAction = "Approve"
	// PromotionTriggerSkip marks a cluster as satisfied for one revision.
	PromotionTriggerSkip PromotionTriggerAction = "Skip"
)

// PromotionApproval is the cross-cluster gate for one cluster.
// Empty means the promoter writes Start without an Approve trigger.
// +kubebuilder:validation:Enum=Approve
type PromotionApproval string

// PromotionTriggerAction is an append-only pipeline trigger.
// +kubebuilder:validation:Enum=Approve;Skip
type PromotionTriggerAction string

// PromotionPipeline orders the clusters a promoter walks.
// The object is cluster-scoped and lives on the management cluster.
type PromotionPipelineSpec struct {
	// Clusters is the promotion order. The first entry is never sent a Start.
	// Each later entry receives a Start after the previous cluster completes
	// the same CompositionRollout name and composition. Names must be unique.
	// The length and name bounds keep that rule inside the apiserver CEL cost limit.
	// Optional per-cluster fields hold that write. Unset fields keep the default.
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(c, self.exists_one(x, x.name == c.name))",message="cluster names must be unique"
	Clusters []PromotionCluster `json:"clusters"`

	// Triggers are append-only. The promoter records them in status and does
	// not delete or edit them. Approve opens a cluster that requires approval.
	// Skip marks a cluster as satisfied for one revision.
	// +listType=map
	// +listMapKey=cluster
	// +listMapKey=action
	// +listMapKey=revision
	// +listMapKey=rollout
	// +listMapKey=composition
	// +optional
	Triggers []PromotionTrigger `json:"triggers,omitempty"`

	// DryRunChain records the Start a spec.dryRun predecessor would cause
	// on the next cluster and does not write it. Unset leaves that hop
	// Blocked with reason DryRun.
	// +optional
	DryRunChain *bool `json:"dryRunChain,omitempty"`
}

// PromotionTrigger is one pipeline trigger. Empty rollout and composition
// match every rollout and composition on that cluster.
type PromotionTrigger struct {
	// Cluster is the pipeline cluster this trigger applies to.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Cluster string `json:"cluster"`

	// +kubebuilder:validation:Enum=Approve;Skip
	Action PromotionTriggerAction `json:"action"`

	// Revision is the CompositionRevision name. Empty does not match.
	// The empty default keeps this list-map key set when callers omit it.
	// +kubebuilder:default=""
	// +optional
	Revision string `json:"revision,omitempty"`

	// Rollout narrows the trigger to one CompositionRollout name.
	// Empty matches every rollout. The empty default is a map key.
	// +kubebuilder:default=""
	// +optional
	Rollout string `json:"rollout,omitempty"`

	// Composition narrows the trigger to one composition.
	// Empty matches every composition. The empty default is a map key.
	// +kubebuilder:default=""
	// +optional
	Composition string `json:"composition,omitempty"`

	// Actor is free-text identification for audit.
	// +optional
	Actor string `json:"actor,omitempty"`
}

// PromotionCluster is one engaged cluster. List order is the promotion order.
// Optional fields are gates for the hop into this cluster. The first cluster
// is never sent a Start, so its gates are unused. Leave them unset on dev
// and test to keep those hops automatic.
type PromotionCluster struct {
	// Name matches the kubeconfig Secret metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Approval, when Approve, waits for an Approve trigger for this cluster
	// and revision after the previous cluster has completed, and before
	// Start is written. Unset writes Start without that trigger.
	// +kubebuilder:validation:Enum=Approve
	// +optional
	Approval PromotionApproval `json:"approval,omitempty"`

	// RevisionMatch holds this hop while this cluster's latest
	// CompositionRevision for the composition differs from the revision
	// that completed on the previous cluster. Start names only that same
	// revision. Unset keeps the existing latest-revision check.
	// +optional
	RevisionMatch *bool `json:"revisionMatch,omitempty"`

	// Soak waits this long after the previous cluster completed before
	// Start is written. It is not the in-cluster canary soak.
	// +optional
	Soak *metav1.Duration `json:"soak,omitempty"`

	// MinCompletedPercent is the percentage of matching CompositionRollouts
	// on the previous cluster that must have completed this revision before
	// Start is written. Matching rollouts govern the same composition.
	// fleetSelector narrows them by label. 100 requires every matching
	// rollout. Unset promotes each CompositionRollout on its own.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +optional
	MinCompletedPercent *int32 `json:"minCompletedPercent,omitempty"`

	// FleetSelector restricts which CompositionRollouts count toward
	// MinCompletedPercent. Unset means every rollout on the previous
	// cluster that governs the composition.
	// +optional
	FleetSelector *metav1.LabelSelector `json:"fleetSelector,omitempty"`
}

// PromotionHopStatus is the promoter's view of one adjacent pair.
// It is rebuilt every reconcile. It is not a log.
type PromotionHopStatus struct {
	Rollout     string `json:"rollout"`
	Composition string `json:"composition"`
	// +optional
	Revision string `json:"revision,omitempty"`
	From     string `json:"from"`
	To       string `json:"to"`
	// Action is None, WriteStart, Wait, Blocked, or Recorded.
	Action string `json:"action"`
	// +optional
	Reason string `json:"reason,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
	// WouldStart is the Start a dry-run predecessor would have appended.
	// The promoter does not write it onto the next cluster.
	// +optional
	WouldStart *RolloutTrigger `json:"wouldStart,omitempty"`
}

// AcknowledgedPromotionTrigger records a pipeline trigger the promoter
// accepted. The spec trigger is left in place.
type AcknowledgedPromotionTrigger struct {
	Cluster string                 `json:"cluster"`
	Action  PromotionTriggerAction `json:"action"`
	// +optional
	Revision string `json:"revision,omitempty"`
	// +optional
	Rollout string `json:"rollout,omitempty"`
	// +optional
	Composition string `json:"composition,omitempty"`
	// Outcome is Applied.
	Outcome string `json:"outcome"`
	// +optional
	AcknowledgedAt *metav1.Time `json:"acknowledgedAt,omitempty"`
}

// PromotionPipelineStatus is the observed hops.
type PromotionPipelineStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	Hops []PromotionHopStatus `json:"hops,omitempty"`
	// AcknowledgedTriggers are pipeline triggers the promoter has accepted.
	// Spec triggers are not removed.
	// +optional
	AcknowledgedTriggers []AcknowledgedPromotionTrigger `json:"acknowledgedTriggers,omitempty"`
}

// ApprovalRequired reports whether this cluster waits for an Approve trigger.
func (c PromotionCluster) ApprovalRequired() bool {
	return c.Approval == PromotionApprovalApprove
}

// RevisionMatchEnabled reports whether a revision mismatch holds the hop.
// Nil is false.
func (c PromotionCluster) RevisionMatchEnabled() bool {
	return c.RevisionMatch != nil && *c.RevisionMatch
}

// DryRunChainEnabled reports whether a dry-run predecessor records Start
// instead of blocking the hop. Nil is false.
func (s *PromotionPipelineSpec) DryRunChainEnabled() bool {
	return s.DryRunChain != nil && *s.DryRunChain
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=xpromote
// +kubebuilder:printcolumn:name="Clusters",type=integer,JSONPath=`.spec.clusters[*].name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// PromotionPipeline is the Schema for cross-cluster promotion order.
type PromotionPipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PromotionPipelineSpec   `json:"spec,omitempty"`
	Status PromotionPipelineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PromotionPipelineList contains a list of PromotionPipeline.
type PromotionPipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PromotionPipeline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PromotionPipeline{}, &PromotionPipelineList{})
}
