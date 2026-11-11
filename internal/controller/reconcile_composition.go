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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/metrics"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

const periodicRequeue = 30 * time.Second

type compositionReconcileResult struct {
	requeueAfter time.Duration
}

func (r *CompositionRolloutReconciler) reconcileComposition(
	ctx context.Context,
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
) (compositionReconcileResult, error) {
	res := compositionReconcileResult{requeueAfter: periodicRequeue}

	latest, err := r.latestCompositionRevision(ctx, target.Name)
	if err != nil {
		return res, nil
	}

	gvk, err := r.compositeTypeGVK(ctx, target.Name)
	if err != nil {
		return res, err
	}
	if err := r.ensureXRWatch(gvk); err != nil {
		return res, err
	}

	xrs, err := r.listXRs(ctx, gvk)
	if err != nil {
		return res, err
	}

	managed, unmanaged, updated, pending := partitionFleet(xrs, target, latest.name)
	state.TotalXRs = int32(len(managed) + len(unmanaged))
	state.UnmanagedXRs = int32(len(unmanaged))
	state.UpdatedXRs = int32(len(updated))

	var healthyCount int32
	for i := range managed {
		if ok, _, _ := xp.IsReadyAndSynced(&managed[i]); ok {
			healthyCount++
		}
	}
	state.HealthyXRs = healthyCount

	metrics.SetXRCounts(cr.Name, target.Name, int32(len(managed)), state.UpdatedXRs, state.HealthyXRs, state.UnmanagedXRs, r.MetricsAggregateOnly)

	trigger := rollout.FindPendingTrigger(cr.Spec.Triggers, state.AcknowledgedTriggers, target.Name)

	if cr.Spec.Suspend {
		state.Phase = rolloutsv1alpha1.PhasePaused
		if state.PauseReason == nil || state.PauseReason.Reason != rolloutsv1alpha1.PauseReasonSuspended {
			state.PauseReason = &rolloutsv1alpha1.PauseReason{Reason: rolloutsv1alpha1.PauseReasonSuspended, Message: "spec.suspend is true"}
		}
		metrics.SetPhase(cr.Name, target.Name, state.Phase, r.MetricsAggregateOnly)
		return res, nil
	}

	in := rollout.Input{
		Phase:                state.Phase,
		Suspend:              cr.Spec.Suspend,
		AutoStart:            cr.Spec.AutoStartEnabled(),
		OnNewRevision:        cr.Spec.OnNewRevision,
		PromotionPolicy:      cr.Spec.Promotion.PromotionPolicyFor(r.Environment),
		LatestRevisionName:   latest.name,
		LatestRevisionNumber: latest.revision,
		TargetRevisionName:   state.TargetRevision.Name,
		TargetRevisionNumber: state.TargetRevision.Revision,
		AbortedRevision:      state.AbortedRevision,
		PhaseBeforePause:     state.PhaseBeforePause,
		Trigger:              trigger,
	}

	in.CanaryAllPatched, in.CanaryFailed = r.evaluateCanaryPatchState(ctx, cr, target, state, managed, latest.name)
	in.CanaryHealthy, in.SoakPassed, in.WaveFailed = r.evaluateHealth(state, cr, managed, pending)

	if state.Phase == rolloutsv1alpha1.PhasePromoting {
		in.WavesComplete = len(pending) == 0 && state.Wave.Current >= state.Wave.Total && state.Wave.Total > 0
		if in.WavesComplete && len(pending) == 0 && len(updated) == len(managed) {
			in.WavesComplete = true
		}
	}

	out := rollout.Transition(in)

	if err := r.applyTransition(ctx, cr, target, state, out, trigger, managed, pending, updated, latest, gvk); err != nil {
		return res, err
	}

	metrics.SetPhase(cr.Name, target.Name, state.Phase, r.MetricsAggregateOnly)
	metrics.SetCanaryUnhealthy(cr.Name, target.Name, state.Canary.Unhealthy, r.MetricsAggregateOnly)

	res.requeueAfter = r.timingRequeue(state)
	return res, nil
}

func (r *CompositionRolloutReconciler) applyTransition(
	ctx context.Context,
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	out rollout.Output,
	trigger *rolloutsv1alpha1.RolloutTrigger,
	managed, pending, updated []unstructured.Unstructured,
	latest revisionRecord,
	gvk schema.GroupVersionKind,
) error {
	prevPhase := state.Phase
	state.Phase = out.Phase
	if out.PauseReason != nil {
		if state.Phase == rolloutsv1alpha1.PhasePaused && prevPhase != rolloutsv1alpha1.PhasePaused {
			metrics.IncPause(cr.Name, target.Name, out.PauseReason.Reason, r.MetricsAggregateOnly)
			r.Recorder.Eventf(cr, "Warning", "RolloutPaused", "composition %s paused: %s", target.Name, out.PauseReason.Message)
		}
		state.PauseReason = out.PauseReason
	} else if state.Phase != rolloutsv1alpha1.PhasePaused {
		state.PauseReason = nil
	}
	state.PhaseBeforePause = out.PhaseBeforePause

	if out.ClearAbort {
		state.AbortedRevision = ""
	}
	if out.MarkAborted {
		state.AbortedRevision = state.TargetRevision.Name
		r.Recorder.Eventf(cr, "Normal", "RolloutAborted", "composition %s rollout aborted", target.Name)
	}
	if out.MarkCompleted {
		now := metav1.Now()
		state.CompletedAt = &now
		r.Recorder.Eventf(cr, "Normal", "RolloutCompleted", "composition %s rollout completed", target.Name)
	}
	if out.Supersede {
		r.Recorder.Eventf(cr, "Normal", "RolloutSuperseded", "composition %s superseded by newer revision", target.Name)
	}

	if out.StartRollout {
		revMap, err := buildRevisionMap(ctx, r, target.Name)
		if err != nil {
			return err
		}
		state.TargetRevision = rolloutsv1alpha1.RevisionInfo{Name: latest.name, Revision: latest.revision}
		if state.SourceRevision.Name == "" {
			state.SourceRevision = fleetSourceRevision(managed, revMap)
		}
		now := metav1.Now()
		state.StartedAt = &now
		state.SoakEndsAt = nil
		state.Wave = rolloutsv1alpha1.WaveStatus{}
		if err := r.startCanaries(ctx, cr, target, state, managed); err != nil {
			return err
		}
		r.Recorder.Eventf(cr, "Normal", "RolloutStarted", "composition %s rollout started for revision %s", target.Name, latest.name)
	}

	if out.EnterSoak {
		now := metav1.Now()
		soakEnd := metav1.NewTime(now.Add(cr.Spec.Soak.Duration.Duration))
		state.SoakEndsAt = &soakEnd
		r.Recorder.Eventf(cr, "Normal", "SoakStarted", "composition %s soak until %s", target.Name, soakEnd.Format(time.RFC3339))
	}

	if out.EnterPromoting {
		state.Wave = rolloutsv1alpha1.WaveStatus{Current: 0, Total: r.waveTotal(len(pending), cr.Spec.Waves.BatchSize)}
		r.Recorder.Eventf(cr, "Normal", "Approved", "composition %s entering promotion", target.Name)
	}

	if out.AcknowledgeTrigger && trigger != nil {
		ack := rolloutsv1alpha1.AcknowledgedTrigger{
			Action:         trigger.Action,
			Revision:       trigger.Revision,
			Outcome:        out.TriggerOutcome,
			AcknowledgedAt: ptrTime(metav1.Now()),
		}
		if ack.Revision == "" {
			ack.Revision = state.TargetRevision.Name
		}
		state.AcknowledgedTriggers = append(state.AcknowledgedTriggers, ack)
		if out.TriggerOutcome == rollout.TriggerOutcomeStale {
			r.Recorder.Event(cr, "Warning", "TriggerIgnored", fmt.Sprintf("stale trigger for composition %s", target.Name))
		}
	}

	switch state.Phase {
	case rolloutsv1alpha1.PhaseCanarying:
		return r.patchCanaries(ctx, cr, target, state, managed, latest.name)
	case rolloutsv1alpha1.PhasePromoting:
		return r.promoteWave(ctx, cr, target, state, pending, latest.name)
	case rolloutsv1alpha1.PhaseIdle,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseCompleted,
		rolloutsv1alpha1.PhaseAborted:
		return nil
	default:
		_ = impossiblePhase(state.Phase)
		return nil
	}
}

func ptrTime(t metav1.Time) *metav1.Time {
	return &t
}

func partitionFleet(
	all []unstructured.Unstructured,
	target rolloutsv1alpha1.CompositionTarget,
	targetRev string,
) (managed, unmanaged, updated, pending []unstructured.Unstructured) {
	var namespaces []string
	var sel *metav1.LabelSelector
	if target.XRSelector != nil {
		namespaces = target.XRSelector.Namespaces
		sel = target.XRSelector.LabelSelector
	}

	for i := range all {
		u := &all[i]
		if xp.GetCompositionRefName(u) != target.Name {
			continue
		}
		if xp.GetCompositionUpdatePolicy(u) != xp.ManualCompositionUpdatePolicy {
			unmanaged = append(unmanaged, *u)
			continue
		}
		if !xp.MatchesXRSelector(u, namespaces, sel) {
			continue
		}
		managed = append(managed, *u)
		ref := xp.GetCompositionRevisionRef(u)
		if ref == targetRev {
			updated = append(updated, *u)
		} else {
			pending = append(pending, *u)
		}
	}
	return managed, unmanaged, updated, pending
}

func (r *CompositionRolloutReconciler) startCanaries(
	ctx context.Context,
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	managed []unstructured.Unstructured,
) error {
	candidates := make([]rollout.Candidate, 0, len(managed))
	for i := range managed {
		u := &managed[i]
		candidates = append(candidates, rollout.Candidate{
			Namespace: u.GetNamespace(),
			Name:      u.GetName(),
			UID:       string(u.GetUID()),
		})
	}

	labelMatch := func(c rollout.Candidate) bool {
		if cr.Spec.Canary.LabelSelector == nil {
			return true
		}
		for i := range managed {
			u := &managed[i]
			if u.GetName() != c.Name || u.GetNamespace() != c.Namespace {
				continue
			}
			return xp.MatchesXRSelector(u, nil, cr.Spec.Canary.LabelSelector)
		}
		return false
	}

	selected, err := rollout.SelectCanaries(
		candidates,
		cr.Spec.Canary.Strategy,
		cr.Spec.Canary.Count,
		cr.Spec.Canary.Percent,
		cr.Spec.Canary.PreferredNamespaces,
		labelMatch,
	)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		state.Phase = rolloutsv1alpha1.PhasePaused
		state.PauseReason = &rolloutsv1alpha1.PauseReason{
			Reason:  rolloutsv1alpha1.PauseReasonCanarySetEmpty,
			Message: "no XRs matched canary selection",
		}
		metrics.IncPause(cr.Name, target.Name, rolloutsv1alpha1.PauseReasonCanarySetEmpty, r.MetricsAggregateOnly)
		return nil
	}

	refs := make([]rolloutsv1alpha1.XRRef, 0, len(selected))
	for i := range selected {
		refs = append(refs, rolloutsv1alpha1.XRRef{Namespace: selected[i].Namespace, Name: selected[i].Name})
	}
	state.Canary = rolloutsv1alpha1.CanaryStatus{Selected: refs}
	r.Recorder.Eventf(cr, "Normal", "CanarySelected", "composition %s selected %d canaries", target.Name, len(refs))
	return nil
}

func (r *CompositionRolloutReconciler) patchCanaries(
	ctx context.Context,
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	managed []unstructured.Unstructured,
	revName string,
) error {
	canarySet := map[string]struct{}{}
	for i := range state.Canary.Selected {
		ref := state.Canary.Selected[i]
		canarySet[rolloutsv1alpha1.XRKey(ref.Namespace, ref.Name)] = struct{}{}
	}

	for i := range managed {
		u := &managed[i]
		key := rolloutsv1alpha1.XRKey(u.GetNamespace(), u.GetName())
		if _, ok := canarySet[key]; !ok {
			continue
		}
		if xp.GetCompositionRevisionRef(u) == revName {
			continue
		}
		if err := r.patchXRRevision(ctx, u, revName); err != nil {
			metrics.IncUpdateErrors(cr.Name, target.Name, r.MetricsAggregateOnly)
			return err
		}
		metrics.IncUpdates(cr.Name, target.Name, r.MetricsAggregateOnly)
	}
	return nil
}

func (r *CompositionRolloutReconciler) promoteWave(
	ctx context.Context,
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	pending []unstructured.Unstructured,
	revName string,
) error {
	if state.Wave.WaveEndsAt != nil && time.Now().Before(state.Wave.WaveEndsAt.Time) {
		return nil
	}

	canarySet := map[string]struct{}{}
	for i := range state.Canary.Selected {
		ref := state.Canary.Selected[i]
		canarySet[rolloutsv1alpha1.XRKey(ref.Namespace, ref.Name)] = struct{}{}
	}

	nonCanaryPending := make([]rollout.Candidate, 0, len(pending))
	for i := range pending {
		u := &pending[i]
		key := rolloutsv1alpha1.XRKey(u.GetNamespace(), u.GetName())
		if _, isCanary := canarySet[key]; isCanary {
			continue
		}
		nonCanaryPending = append(nonCanaryPending, rollout.Candidate{
			Namespace: u.GetNamespace(),
			Name:      u.GetName(),
			UID:       string(u.GetUID()),
		})
	}

	batch := rollout.ComputeBatchSize(len(nonCanaryPending), cr.Spec.Waves.BatchSize)
	wave := rollout.SelectNextWave(nonCanaryPending, batch)
	if len(wave) == 0 {
		return nil
	}

	sem := semaphore.NewWeighted(int64(cr.Spec.Waves.MaxConcurrent()))
	eg, egCtx := errgroup.WithContext(ctx)
	for i := range wave {
		c := wave[i]
		if err := sem.Acquire(egCtx, 1); err != nil {
			return err
		}
		eg.Go(func() error {
			defer sem.Release(1)
			u, ok := findXR(pending, c.Namespace, c.Name)
			if !ok {
				return nil
			}
			if err := r.patchXRRevision(egCtx, u, revName); err != nil {
				metrics.IncUpdateErrors(cr.Name, target.Name, r.MetricsAggregateOnly)
				return err
			}
			metrics.IncUpdates(cr.Name, target.Name, r.MetricsAggregateOnly)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		state.Phase = rolloutsv1alpha1.PhasePaused
		state.PauseReason = &rolloutsv1alpha1.PauseReason{
			Reason:  rolloutsv1alpha1.PauseReasonPatchFailed,
			Message: err.Error(),
		}
		return nil
	}

	state.Wave.Current++
	if state.Wave.Total == 0 {
		state.Wave.Total = r.waveTotal(len(nonCanaryPending), cr.Spec.Waves.BatchSize)
	}
	now := metav1.Now()
	end := metav1.NewTime(now.Add(cr.Spec.Waves.WaveSoak.Duration))
	state.Wave.WaveEndsAt = &end
	r.Recorder.Eventf(cr, "Normal", "WaveStarted", "composition %s wave %d", target.Name, state.Wave.Current)
	return nil
}

func findXR(items []unstructured.Unstructured, ns, name string) (*unstructured.Unstructured, bool) {
	for i := range items {
		if items[i].GetNamespace() == ns && items[i].GetName() == name {
			return &items[i], true
		}
	}
	return nil, false
}

func (r *CompositionRolloutReconciler) waveTotal(pendingCount int, batch intstr.IntOrString) int32 {
	n := pendingCount
	if n == 0 {
		return 0
	}
	batchSize := rollout.ComputeBatchSize(n, batch)
	total := 0
	rem := n
	for rem > 0 {
		w := rollout.ComputeBatchSize(rem, batch)
		if w == 0 {
			w = batchSize
		}
		if w > rem {
			w = rem
		}
		total++
		rem -= w
	}
	return int32(total)
}
