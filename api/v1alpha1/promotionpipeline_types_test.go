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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestPromotionPipelineRegisters(t *testing.T) {
	t.Parallel()
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	gvk := GroupVersion.WithKind("PromotionPipeline")
	obj, err := s.New(gvk)
	if err != nil {
		t.Fatalf("New(%s): %v", gvk, err)
	}
	if _, ok := obj.(*PromotionPipeline); !ok {
		t.Fatalf("got %T, want *PromotionPipeline", obj)
	}
}

func TestPromotionGatesDefaultOff(t *testing.T) {
	t.Parallel()
	cluster := PromotionCluster{Name: "dev"}
	if cluster.ApprovalRequired() || cluster.RevisionMatchEnabled() {
		t.Fatalf("unset cluster gates = approval %v revisionMatch %v", cluster.ApprovalRequired(), cluster.RevisionMatchEnabled())
	}
	spec := PromotionPipelineSpec{}
	if spec.DryRunChainEnabled() {
		t.Fatal("unset dryRunChain was enabled")
	}

	on := true
	percent := int32(50)
	cluster.Approval = PromotionApprovalApprove
	cluster.RevisionMatch = &on
	cluster.MinCompletedPercent = &percent
	spec.DryRunChain = &on
	if !cluster.ApprovalRequired() || !cluster.RevisionMatchEnabled() || !spec.DryRunChainEnabled() {
		t.Fatalf("set gates = approval %v revisionMatch %v dryRun %v", cluster.ApprovalRequired(), cluster.RevisionMatchEnabled(), spec.DryRunChainEnabled())
	}
	if cluster.Soak != nil {
		t.Fatal("soak was set")
	}
	cluster.Soak = &metav1.Duration{Duration: 0}
	if cluster.MinCompletedPercent == nil || *cluster.MinCompletedPercent != 50 {
		t.Fatalf("percent = %#v", cluster.MinCompletedPercent)
	}
}
