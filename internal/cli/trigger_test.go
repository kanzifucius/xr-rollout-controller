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

package cli

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestRolloutTriggersForApplyKeepsOwnedEntries(t *testing.T) {
	t.Parallel()
	const composition = "platformdb.example.org"
	live := &rolloutsv1alpha1.CompositionRollout{}
	live.Spec.Triggers = []rolloutsv1alpha1.RolloutTrigger{
		{Composition: composition, Action: rolloutsv1alpha1.TriggerStart, Revision: "rev-old", Actor: "xr-rollout-promoter"},
		{Composition: composition, Action: rolloutsv1alpha1.TriggerApprove, Revision: "rev-a", Actor: "first"},
	}
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    FieldManager,
		Operation:  metav1.ManagedFieldsOperationApply,
		APIVersion: rolloutsv1alpha1.GroupVersion.String(),
		FieldsV1:   rolloutFieldsV1(composition, string(rolloutsv1alpha1.TriggerApprove), "rev-a"),
	}})

	got := rolloutTriggersForApply(live, rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerAbort,
		Revision:    "rev-b",
		Actor:       "cli",
	})
	want := []rolloutsv1alpha1.RolloutTrigger{
		{Composition: composition, Action: rolloutsv1alpha1.TriggerApprove, Revision: "rev-a", Actor: "first"},
		{Composition: composition, Action: rolloutsv1alpha1.TriggerAbort, Revision: "rev-b", Actor: "cli"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("apply triggers = %#v, want owned Approve kept and promoter Start omitted", got)
	}
}

func TestRolloutTriggersForApplyReplacesSameKey(t *testing.T) {
	t.Parallel()
	const composition = "platformdb.example.org"
	live := &rolloutsv1alpha1.CompositionRollout{}
	live.Spec.Triggers = []rolloutsv1alpha1.RolloutTrigger{
		{Composition: composition, Action: rolloutsv1alpha1.TriggerApprove, Revision: "rev-a", Actor: "first"},
	}
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:   FieldManager,
		Operation: metav1.ManagedFieldsOperationApply,
		FieldsV1:  rolloutFieldsV1(composition, string(rolloutsv1alpha1.TriggerApprove), "rev-a"),
	}})

	got := rolloutTriggersForApply(live, rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerApprove,
		Revision:    "rev-a",
		Actor:       "second",
	})
	want := []rolloutsv1alpha1.RolloutTrigger{
		{Composition: composition, Action: rolloutsv1alpha1.TriggerApprove, Revision: "rev-a", Actor: "second"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("apply triggers = %#v, want one Approve with the new actor", got)
	}
}

func TestRolloutTriggersForApplyOmitsUnownedEntries(t *testing.T) {
	t.Parallel()
	const composition = "platformdb.example.org"
	live := &rolloutsv1alpha1.CompositionRollout{}
	live.Spec.Triggers = []rolloutsv1alpha1.RolloutTrigger{
		{Composition: composition, Action: rolloutsv1alpha1.TriggerStart, Revision: "rev-old", Actor: "xr-rollout-promoter"},
	}

	got := rolloutTriggersForApply(live, rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerAbort,
		Revision:    "rev-b",
		Actor:       "cli",
	})
	want := []rolloutsv1alpha1.RolloutTrigger{
		{Composition: composition, Action: rolloutsv1alpha1.TriggerAbort, Revision: "rev-b", Actor: "cli"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("apply triggers = %#v, want only the new entry", got)
	}
}

func TestPipelineTriggersForApplyKeepsOwnedEntries(t *testing.T) {
	t.Parallel()
	live := &rolloutsv1alpha1.PromotionPipeline{}
	live.Spec.Triggers = []rolloutsv1alpha1.PromotionTrigger{
		{Cluster: "acc", Action: rolloutsv1alpha1.PromotionTriggerApprove, Revision: "rev-1", Actor: "release"},
		{Cluster: "test", Action: rolloutsv1alpha1.PromotionTriggerSkip, Revision: "rev-1", Rollout: "team-payments", Actor: "cli"},
	}
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:   FieldManager,
		Operation: metav1.ManagedFieldsOperationApply,
		FieldsV1:  pipelineFieldsV1("test", string(rolloutsv1alpha1.PromotionTriggerSkip), "rev-1", "team-payments", ""),
	}})

	got := pipelineTriggersForApply(live, rolloutsv1alpha1.PromotionTrigger{
		Cluster:  "test",
		Action:   rolloutsv1alpha1.PromotionTriggerSkip,
		Revision: "rev-2",
		Actor:    "cli",
	})
	want := []rolloutsv1alpha1.PromotionTrigger{
		{Cluster: "test", Action: rolloutsv1alpha1.PromotionTriggerSkip, Revision: "rev-1", Rollout: "team-payments", Actor: "cli"},
		{Cluster: "test", Action: rolloutsv1alpha1.PromotionTriggerSkip, Revision: "rev-2", Actor: "cli"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("apply triggers = %#v, want owned Skip kept and pipeline Approve omitted", got)
	}
}

func rolloutFieldsV1(composition, action, revision string) *metav1.FieldsV1 {
	key := fmt.Sprintf("k:{\"action\":%q,\"composition\":%q,\"revision\":%q}", action, composition, revision)
	raw := fmt.Sprintf(`{"f:spec":{"f:triggers":{%q:{".":{},"f:action":{},"f:composition":{},"f:revision":{}}}}}`, key)
	return &metav1.FieldsV1{Raw: []byte(raw)}
}

func pipelineFieldsV1(cluster, action, revision, rolloutName, composition string) *metav1.FieldsV1 {
	key := fmt.Sprintf(
		"k:{\"action\":%q,\"cluster\":%q,\"composition\":%q,\"revision\":%q,\"rollout\":%q}",
		action, cluster, composition, revision, rolloutName,
	)
	raw := fmt.Sprintf(`{"f:spec":{"f:triggers":{%q:{".":{},"f:action":{},"f:cluster":{},"f:revision":{}}}}}`, key)
	return &metav1.FieldsV1{Raw: []byte(raw)}
}
