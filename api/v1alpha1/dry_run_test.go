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

import "testing"

func TestDryRunEnabled(t *testing.T) {
	t.Parallel()
	on := true
	off := false
	cases := []struct {
		name string
		spec CompositionRolloutSpec
		want bool
	}{
		{name: "omitted", spec: CompositionRolloutSpec{}, want: false},
		{name: "false", spec: CompositionRolloutSpec{DryRun: &off}, want: false},
		{name: "true", spec: CompositionRolloutSpec{DryRun: &on}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.spec.DryRunEnabled(); got != tc.want {
				t.Fatalf("DryRunEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSimulationNilReport(t *testing.T) {
	t.Parallel()
	state := CompositionRolloutState{}
	if got := state.Simulation(); got != nil {
		t.Fatalf("Simulation() = %#v, want nil", got)
	}
	state.DryRun = &DryRunStatus{WouldUpdate: []DryRunUpdate{{Name: "a", Revision: "rev", Stage: DryRunStageCanary}}}
	if len(state.Simulation()) != 1 {
		t.Fatalf("Simulation() = %#v, want one entry", state.Simulation())
	}
}
