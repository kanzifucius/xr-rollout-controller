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
	"regexp"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

func (r *CompositionRolloutReconciler) listRolloutsForComposition(ctx context.Context, compName string) ([]rolloutsv1alpha1.CompositionRollout, error) {
	var list rolloutsv1alpha1.CompositionRolloutList
	if err := r.List(ctx, &list, client.MatchingFields{FieldIndexCompositionName: compName}); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (r *CompositionRolloutReconciler) patchXRRevision(ctx context.Context, u *unstructured.Unstructured, revName string) error {
	// Apply only the revision name. A full-object apply would claim every other field.
	patchBase := &unstructured.Unstructured{}
	patchBase.SetGroupVersionKind(u.GroupVersionKind())
	patchBase.SetNamespace(u.GetNamespace())
	patchBase.SetName(u.GetName())
	xp.SetCompositionRevisionRef(patchBase, revName)
	opts := []client.PatchOption{client.FieldOwner(r.FieldManager)}
	if r.ForceOwnership {
		opts = append(opts, client.ForceOwnership)
	}
	err := r.Patch(ctx, patchBase, client.Apply, opts...)
	if err == nil || r.ForceOwnership || !crossplaneRevisionConflict(err) {
		return err
	}
	// Crossplane applies this field itself. Take it so a Manual rollout can
	// change the revision. Any other manager still surfaces as PatchFailed.
	opts = append(opts, client.ForceOwnership)
	return r.Patch(ctx, patchBase, client.Apply, opts...)
}

var conflictManagerPattern = regexp.MustCompile(`conflict with "([^"]+)"`)

func crossplaneRevisionConflict(err error) bool {
	if err == nil || !apierrors.IsConflict(err) {
		return false
	}
	managers := conflictManagerPattern.FindAllStringSubmatch(err.Error(), -1)
	if len(managers) == 0 {
		return false
	}
	for _, match := range managers {
		switch match[1] {
		case "crossplane", "apiextensions.crossplane.io/composite":
		default:
			return false
		}
	}
	return true
}

func (r *CompositionRolloutReconciler) evaluateCanaryPatchState(
	_ context.Context,
	cr *rolloutsv1alpha1.CompositionRollout,
	_ rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	managed []unstructured.Unstructured,
	targetRev string,
) (allPatched bool, failed bool) {
	if len(state.Canary.Selected) == 0 {
		return false, false
	}
	canarySet := map[string]struct{}{}
	for i := range state.Canary.Selected {
		ref := state.Canary.Selected[i]
		canarySet[rolloutsv1alpha1.XRKey(ref.Namespace, ref.Name)] = struct{}{}
	}

	var patched, unhealthy int32
	for i := range managed {
		u := &managed[i]
		key := rolloutsv1alpha1.XRKey(u.GetNamespace(), u.GetName())
		if _, ok := canarySet[key]; !ok {
			continue
		}
		patchedNow := xp.GetCompositionRevisionRef(u) == targetRev
		if !patchedNow && cr.Spec.DryRunEnabled() && rollout.IsSimulated(state.Simulation(), u.GetNamespace(), u.GetName(), targetRev) {
			patchedNow = true
		}
		if patchedNow {
			patched++
		}
		ok, _, _ := xp.IsReadyAndSynced(u)
		if !ok {
			unhealthy++
		}
	}
	state.Canary.Healthy = patched - unhealthy
	if state.Canary.Healthy < 0 {
		state.Canary.Healthy = 0
	}
	state.Canary.Unhealthy = unhealthy

	allPatched = int(patched) == len(state.Canary.Selected)
	if unhealthy > 0 && state.Phase == rolloutsv1alpha1.PhaseCanarying {
		failed = unhealthy > 0
	}

	if state.StartedAt != nil {
		timeout := 30 * time.Minute
		if cr.Spec.Soak.StabilisationTimeout != nil {
			timeout = cr.Spec.Soak.StabilisationTimeout.Duration
		}
		if !allPatched && time.Since(state.StartedAt.Time) > timeout {
			failed = true
		}
	}
	return allPatched, failed
}

func (r *CompositionRolloutReconciler) evaluateHealth(
	state *rolloutsv1alpha1.CompositionRolloutState,
	cr *rolloutsv1alpha1.CompositionRollout,
	managed, pending []unstructured.Unstructured,
) (canaryHealthy bool, soakPassed bool, soakFailed bool, waveFailed bool) {
	canarySet := map[string]struct{}{}
	for i := range state.Canary.Selected {
		ref := state.Canary.Selected[i]
		canarySet[rolloutsv1alpha1.XRKey(ref.Namespace, ref.Name)] = struct{}{}
	}

	items := make([]rollout.HealthItem, 0, len(canarySet))
	for i := range managed {
		u := &managed[i]
		key := rolloutsv1alpha1.XRKey(u.GetNamespace(), u.GetName())
		if _, ok := canarySet[key]; !ok {
			continue
		}
		_, ready, synced := xp.IsReadyAndSynced(u)
		item := rollout.HealthItem{}
		if ready != nil {
			item.ReadyTrue = ready.Status == metav1.ConditionTrue
			item.ReadySince = ready.LastTransitionTime.Time
		}
		if synced != nil {
			item.SyncedTrue = synced.Status == metav1.ConditionTrue
			item.SyncedSince = synced.LastTransitionTime.Time
		}
		items = append(items, item)
	}

	soakStart := time.Now()
	if state.StartedAt != nil {
		soakStart = state.StartedAt.Time
	}
	if state.SoakEndsAt != nil && state.Phase == rolloutsv1alpha1.PhaseSoaking {
		healthy, unhealthy, failed := rollout.EvaluateSoak(items, soakStart, time.Now(), cr.Spec.Soak.MaxUnhealthy)
		state.Canary.Healthy = healthy
		state.Canary.Unhealthy = unhealthy
		canaryHealthy = !failed && unhealthy == 0
		soakFailed = failed
		if state.SoakEndsAt != nil && time.Now().After(state.SoakEndsAt.Time) && !failed {
			soakPassed = true
		}
	}

	if state.Phase == rolloutsv1alpha1.PhasePromoting && state.Wave.WaveEndsAt != nil {
		var waveItems []rollout.HealthItem
		for i := range managed {
			u := &managed[i]
			ok, ready, synced := xp.IsReadyAndSynced(u)
			item := rollout.HealthItem{ReadyTrue: ok, SyncedTrue: ok}
			if ready != nil {
				item.ReadySince = ready.LastTransitionTime.Time
			}
			if synced != nil {
				item.SyncedSince = synced.LastTransitionTime.Time
			}
			waveItems = append(waveItems, item)
		}
		_, unhealthy, failed := rollout.EvaluateSoak(waveItems, state.Wave.WaveEndsAt.Time, time.Now(), cr.Spec.Waves.MaxUnhealthy)
		_ = pending
		waveFailed = failed || unhealthy > cr.Spec.Waves.MaxUnhealthy
	}
	return canaryHealthy, soakPassed, soakFailed, waveFailed
}

func targetRevisionMissing(ctx context.Context, r *CompositionRolloutReconciler, composition string, state *rolloutsv1alpha1.CompositionRolloutState) (bool, error) {
	if state.TargetRevision.Name == "" || !phaseTracksTargetRevision(state.Phase) {
		return false, nil
	}
	revMap, err := buildRevisionMap(ctx, r, composition)
	if err != nil {
		return false, err
	}
	_, ok := revMap[state.TargetRevision.Name]
	return !ok, nil
}

func phaseTracksTargetRevision(phase rolloutsv1alpha1.RolloutPhase) bool {
	switch phase {
	case rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePromoting:
		return true
	case rolloutsv1alpha1.PhaseIdle,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseCompleted,
		rolloutsv1alpha1.PhaseAborted:
		return false
	default:
		_ = impossiblePhase(phase)
		return false
	}
}

func (r *CompositionRolloutReconciler) timingRequeue(state *rolloutsv1alpha1.CompositionRolloutState) time.Duration {
	now := time.Now()
	switch state.Phase {
	case rolloutsv1alpha1.PhaseSoaking:
		if state.SoakEndsAt != nil && state.SoakEndsAt.After(now) {
			return time.Until(state.SoakEndsAt.Time)
		}
	case rolloutsv1alpha1.PhasePromoting:
		if state.Wave.WaveEndsAt != nil && state.Wave.WaveEndsAt.After(now) {
			return time.Until(state.Wave.WaveEndsAt.Time)
		}
	case rolloutsv1alpha1.PhaseIdle,
		rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseCompleted,
		rolloutsv1alpha1.PhaseAborted:
	default:
		_ = impossiblePhase(state.Phase)
	}
	return periodicRequeue
}

func findCompositionState(status *rolloutsv1alpha1.CompositionRolloutStatus, name string) *rolloutsv1alpha1.CompositionRolloutState {
	for i := range status.Rollouts {
		if status.Rollouts[i].Composition == name {
			return &status.Rollouts[i]
		}
	}
	st := rolloutsv1alpha1.CompositionRolloutState{
		Composition: name,
		Phase:       rolloutsv1alpha1.PhaseIdle,
	}
	status.Rollouts = append(status.Rollouts, st)
	return &status.Rollouts[len(status.Rollouts)-1]
}

func compositionNamesOverlap(a, b []rolloutsv1alpha1.CompositionTarget) (string, bool) {
	set := map[string]struct{}{}
	for i := range a {
		set[a[i].Name] = struct{}{}
	}
	for i := range b {
		if _, ok := set[b[i].Name]; ok {
			return b[i].Name, true
		}
	}
	return "", false
}
