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

import rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"

// IsSimulated reports whether the would-update log already contains this XR
// for this CompositionRevision name.
func IsSimulated(updates []rolloutsv1alpha1.DryRunUpdate, namespace, name, revision string) bool {
	for i := range updates {
		item := updates[i]
		if item.Namespace == namespace && item.Name == name && item.Revision == revision {
			return true
		}
	}
	return false
}

// RecordUpdate appends next when that namespace, name, and revision are new.
// The same XR for a different revision is a new entry.
func RecordUpdate(updates []rolloutsv1alpha1.DryRunUpdate, next rolloutsv1alpha1.DryRunUpdate) ([]rolloutsv1alpha1.DryRunUpdate, bool) {
	if IsSimulated(updates, next.Namespace, next.Name, next.Revision) {
		return updates, false
	}
	return append(updates, next), true
}

// SuppressDryRunCompletion turns a completed transition into Idle and tells
// the caller to stamp dryRun.completedAt instead of the rollout completedAt.
// The second result is true only when that swap happens.
func SuppressDryRunCompletion(out Output, dryRun bool) (Output, bool) {
	if !dryRun || !out.MarkCompleted {
		return out, false
	}
	out.Phase = rolloutsv1alpha1.PhaseIdle
	out.MarkCompleted = false
	return out, true
}

// ShouldResetDryRun reports that the flag was cleared after a simulation.
// Suspend wins: the caller keeps the suspended snapshot until suspend is cleared.
func ShouldResetDryRun(specDryRun, reportActive, suspend bool) bool {
	return !suspend && !specDryRun && reportActive
}

// ResetSimulationLog returns a fresh active report for a new simulated rollout.
// It does not reuse the previous would-update entries.
func ResetSimulationLog(prev *rolloutsv1alpha1.DryRunStatus, promotion rolloutsv1alpha1.PromotionPolicy) *rolloutsv1alpha1.DryRunStatus {
	_ = prev
	return &rolloutsv1alpha1.DryRunStatus{
		Active:    true,
		Promotion: promotion,
	}
}
