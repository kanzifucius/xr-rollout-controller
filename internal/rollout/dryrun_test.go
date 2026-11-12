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

package rollout

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestRecordUpdate(t *testing.T) {
	t.Parallel()
	first := rolloutsv1alpha1.DryRunUpdate{
		Namespace: "payments", Name: "a", Stage: rolloutsv1alpha1.DryRunStageCanary, Revision: "rev-1",
	}
	again := first
	wave := rolloutsv1alpha1.DryRunUpdate{
		Namespace: "payments", Name: "b", Stage: rolloutsv1alpha1.DryRunStageWave, Wave: 1, Revision: "rev-1",
	}
	otherRev := first
	otherRev.Revision = "rev-2"

	got, added := RecordUpdate(nil, first)
	if !added || len(got) != 1 {
		t.Fatalf("first record = (%d, %v), want (1, true)", len(got), added)
	}
	got, added = RecordUpdate(got, again)
	if added || len(got) != 1 {
		t.Fatalf("duplicate = (%d, %v), want (1, false)", len(got), added)
	}
	got, added = RecordUpdate(got, wave)
	if !added || len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("wave append = %#v added %v", got, added)
	}
	got, added = RecordUpdate(got, otherRev)
	if !added || len(got) != 3 {
		t.Fatalf("other revision = (%d, %v), want (3, true)", len(got), added)
	}
	if !IsSimulated(got, "payments", "a", "rev-1") || IsSimulated(got, "payments", "a", "missing") {
		t.Fatalf("IsSimulated = %#v", got)
	}
	if IsSimulated(got, "other", "a", "rev-1") {
		t.Fatalf("namespace mismatch counted as simulated")
	}
}

func TestSuppressDryRunCompletion(t *testing.T) {
	t.Parallel()
	completed := Output{Phase: rolloutsv1alpha1.PhaseCompleted, MarkCompleted: true}
	got, done := SuppressDryRunCompletion(completed, true)
	if !done || got.MarkCompleted || got.Phase != rolloutsv1alpha1.PhaseIdle {
		t.Fatalf("dry completion = %#v done %v", got, done)
	}
	got, done = SuppressDryRunCompletion(completed, false)
	if done || !got.MarkCompleted || got.Phase != rolloutsv1alpha1.PhaseCompleted {
		t.Fatalf("real completion = %#v done %v", got, done)
	}
	idle := Output{Phase: rolloutsv1alpha1.PhaseIdle}
	got, done = SuppressDryRunCompletion(idle, true)
	if done || got.Phase != rolloutsv1alpha1.PhaseIdle {
		t.Fatalf("idle = %#v done %v", got, done)
	}
}

func TestShouldResetDryRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                        string
		specDryRun, active, suspend bool
		want                        bool
	}{
		{name: "still simulating", specDryRun: true, active: true, want: false},
		{name: "flag cleared", active: true, want: true},
		{name: "flag cleared while suspended", active: true, suspend: true, want: false},
		{name: "never simulated", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ShouldResetDryRun(tc.specDryRun, tc.active, tc.suspend)
			if got != tc.want {
				t.Fatalf("ShouldResetDryRun() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResetSimulationLogDropsPreviousEntries(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Unix(10, 0))
	prev := &rolloutsv1alpha1.DryRunStatus{
		Active:      true,
		CompletedAt: &now,
		WouldUpdate: []rolloutsv1alpha1.DryRunUpdate{{Name: "a", Revision: "old", Stage: rolloutsv1alpha1.DryRunStageCanary}},
	}
	got := ResetSimulationLog(prev, rolloutsv1alpha1.PromotionManualApproval)
	if got == nil || !got.Active || got.CompletedAt != nil || len(got.WouldUpdate) != 0 {
		t.Fatalf("ResetSimulationLog() = %#v", got)
	}
	if got.Promotion != rolloutsv1alpha1.PromotionManualApproval {
		t.Fatalf("promotion = %s", got.Promotion)
	}
	if len(prev.WouldUpdate) != 1 {
		t.Fatalf("previous log was mutated: %#v", prev)
	}
}
