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
	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
)

// applyRevision records a simulated patch, or calls patch when dryRun is false.
// added is true only when a new would-update entry was recorded.
func applyRevision(
	dryRun bool,
	state *rolloutsv1alpha1.CompositionRolloutState,
	ref rolloutsv1alpha1.XRRef,
	revName string,
	stage rolloutsv1alpha1.DryRunStage,
	wave int32,
	patch func() error,
) (bool, error) {
	if !dryRun {
		if err := patch(); err != nil {
			return false, err
		}
		return false, nil
	}
	if state.DryRun == nil {
		state.DryRun = &rolloutsv1alpha1.DryRunStatus{}
	}
	state.DryRun.Active = true
	next := rolloutsv1alpha1.DryRunUpdate{
		Namespace: ref.Namespace,
		Name:      ref.Name,
		Stage:     stage,
		Wave:      wave,
		Revision:  revName,
	}
	updated, added := rollout.RecordUpdate(state.DryRun.WouldUpdate, next)
	state.DryRun.WouldUpdate = updated
	return added, nil
}
