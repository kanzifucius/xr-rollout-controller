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
)

// PromotionPipeline orders the clusters a promoter walks.
// The object is cluster-scoped and lives on the management cluster.
type PromotionPipelineSpec struct {
	// Clusters is the promotion order. The first entry is never sent a Start.
	// Each later entry receives a Start after the previous cluster completes
	// the same CompositionRollout name and composition.
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, self.exists_one(x, x.name == c.name))",message="cluster names must be unique"
	Clusters []PromotionCluster `json:"clusters"`
}

// PromotionCluster is one engaged cluster. List order is the promotion order.
type PromotionCluster struct {
	// Name matches the kubeconfig Secret metadata.name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
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
