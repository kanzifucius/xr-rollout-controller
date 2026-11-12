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

package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

func TestApplyRevisionDryRunDoesNotPatch(t *testing.T) {
	t.Parallel()
	state := &rolloutsv1alpha1.CompositionRolloutState{}
	called := false
	ref := rolloutsv1alpha1.XRRef{Namespace: "payments", Name: "orders"}
	added, err := applyRevision(true, state, ref, "rev-9", rolloutsv1alpha1.DryRunStageCanary, 0, func() error {
		called = true
		return nil
	})
	if err != nil || !added || called {
		t.Fatalf("applyRevision() = (%v, %v) called %v", added, err, called)
	}
	if len(state.Simulation()) != 1 || state.Simulation()[0].Revision != "rev-9" {
		t.Fatalf("log = %#v", state.DryRun)
	}
	added, err = applyRevision(true, state, ref, "rev-9", rolloutsv1alpha1.DryRunStageCanary, 0, func() error {
		called = true
		return nil
	})
	if err != nil || added || called || len(state.Simulation()) != 1 {
		t.Fatalf("second apply = (%v, %v) called %v log %d", added, err, called, len(state.Simulation()))
	}
}

func TestApplyRevisionRealRunCallsPatch(t *testing.T) {
	t.Parallel()
	state := &rolloutsv1alpha1.CompositionRolloutState{
		DryRun: &rolloutsv1alpha1.DryRunStatus{WouldUpdate: []rolloutsv1alpha1.DryRunUpdate{{
			Name: "keep", Revision: "old", Stage: rolloutsv1alpha1.DryRunStageCanary,
		}}},
	}
	called := false
	added, err := applyRevision(false, state, rolloutsv1alpha1.XRRef{Name: "orders"}, "rev-9", rolloutsv1alpha1.DryRunStageWave, 1, func() error {
		called = true
		return nil
	})
	if err != nil || added || !called {
		t.Fatalf("applyRevision() = (%v, %v) called %v", added, err, called)
	}
	if len(state.Simulation()) != 1 || state.Simulation()[0].Name != "keep" {
		t.Fatalf("real run changed the log: %#v", state.DryRun)
	}
}

func TestPatchCanariesDryRunLeavesTheXRUntouched(t *testing.T) {
	t.Parallel()
	rec := record.NewFakeRecorder(4)
	r := &CompositionRolloutReconciler{Recorder: rec}
	on := true
	cr := &rolloutsv1alpha1.CompositionRollout{}
	cr.Name = "team-payments"
	cr.Spec.DryRun = &on
	state := &rolloutsv1alpha1.CompositionRolloutState{
		Canary: rolloutsv1alpha1.CanaryStatus{Selected: []rolloutsv1alpha1.XRRef{{Namespace: "payments", Name: "orders"}}},
		DryRun: &rolloutsv1alpha1.DryRunStatus{Active: true},
	}
	xr := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.org/v1",
		"kind":       "XDatabase",
		"metadata": map[string]interface{}{
			"name":      "orders",
			"namespace": "payments",
		},
	}}
	xp.SetCompositionRevisionRef(xr, "rev-old")
	_ = unstructured.SetNestedField(xr.Object, "Manual", "spec", "crossplane", "compositionUpdatePolicy")

	managed := []unstructured.Unstructured{*xr}
	err := r.patchCanaries(context.Background(), cr, rolloutsv1alpha1.CompositionTarget{Name: "platformdb.example.org"}, state, managed, "rev-new")
	if err != nil {
		t.Fatalf("patchCanaries: %v", err)
	}
	if xp.GetCompositionRevisionRef(&managed[0]) != "rev-old" {
		t.Fatalf("revision ref = %s, want rev-old", xp.GetCompositionRevisionRef(&managed[0]))
	}
	if xp.GetCompositionUpdatePolicy(&managed[0]) != "Manual" {
		t.Fatalf("policy = %s, want Manual", xp.GetCompositionUpdatePolicy(&managed[0]))
	}
	if len(state.Simulation()) != 1 || state.Simulation()[0].Stage != rolloutsv1alpha1.DryRunStageCanary {
		t.Fatalf("log = %#v", state.DryRun)
	}
	select {
	case ev := <-rec.Events:
		if !strings.Contains(ev, "DryRun") {
			t.Fatalf("event = %s, want DryRun", ev)
		}
	default:
		t.Fatal("expected a DryRun event")
	}
}

func TestSimulatedPendingIgnoresTheLogWhenDryRunIsOff(t *testing.T) {
	t.Parallel()
	pending := []unstructured.Unstructured{xrNamed("payments", "a"), xrNamed("payments", "b")}
	log := []rolloutsv1alpha1.DryRunUpdate{{
		Namespace: "payments", Name: "a", Revision: "rev-new", Stage: rolloutsv1alpha1.DryRunStageCanary,
	}}
	if got := simulatedPending(pending, log, "rev-new", false); len(got) != 2 {
		t.Fatalf("live pending = %d, want 2", len(got))
	}
	got := simulatedPending(pending, log, "rev-new", true)
	if len(got) != 1 || got[0].GetName() != "b" {
		t.Fatalf("virtual pending = %v", namesOf(got))
	}
}

func TestEvaluateCanaryPatchStateTreatsSimulationAsPatched(t *testing.T) {
	t.Parallel()
	r := &CompositionRolloutReconciler{}
	on := true
	cr := &rolloutsv1alpha1.CompositionRollout{}
	cr.Spec.DryRun = &on
	state := &rolloutsv1alpha1.CompositionRolloutState{
		Phase: rolloutsv1alpha1.PhaseCanarying,
		Canary: rolloutsv1alpha1.CanaryStatus{
			Selected: []rolloutsv1alpha1.XRRef{{Namespace: "payments", Name: "a"}},
		},
		DryRun: &rolloutsv1alpha1.DryRunStatus{
			Active: true,
			WouldUpdate: []rolloutsv1alpha1.DryRunUpdate{{
				Namespace: "payments", Name: "a", Revision: "rev-new", Stage: rolloutsv1alpha1.DryRunStageCanary,
			}},
		},
	}
	managed := []unstructured.Unstructured{xrNamed("payments", "a")}
	xp.SetCompositionRevisionRef(&managed[0], "rev-old")
	readySince := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	_ = unstructured.SetNestedSlice(managed[0].Object, []interface{}{
		map[string]interface{}{"type": "Ready", "status": "True", "lastTransitionTime": readySince},
		map[string]interface{}{"type": "Synced", "status": "True", "lastTransitionTime": readySince},
	}, "status", "conditions")

	allPatched, failed := r.evaluateCanaryPatchState(context.Background(), cr, rolloutsv1alpha1.CompositionTarget{}, state, managed, "rev-new")
	if !allPatched || failed {
		t.Fatalf("dry run patched=%v failed=%v", allPatched, failed)
	}
	if xp.GetCompositionRevisionRef(&managed[0]) != "rev-old" {
		t.Fatalf("evaluation wrote the ref: %s", xp.GetCompositionRevisionRef(&managed[0]))
	}

	off := false
	cr.Spec.DryRun = &off
	allPatched, failed = r.evaluateCanaryPatchState(context.Background(), cr, rolloutsv1alpha1.CompositionTarget{}, state, managed, "rev-new")
	if allPatched {
		t.Fatalf("real evaluation treated the log as a patch")
	}
}

func xrNamed(namespace, name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}

func namesOf(items []unstructured.Unstructured) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, items[i].GetName())
	}
	return out
}
