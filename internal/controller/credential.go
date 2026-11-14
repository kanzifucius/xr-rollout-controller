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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/metrics"
)

// credentialUnavailableError pauses one rollout. The Secret is missing,
// outside the controller namespace, or its kubeconfig will not parse.
type credentialUnavailableError struct {
	message string
}

func (e *credentialUnavailableError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

// kubeconfigRef is the Secret this rollout uses.
// An unset ref uses spec.cluster in the controller namespace.
func kubeconfigRef(cr *rolloutsv1alpha1.CompositionRollout, controllerNamespace string) (name, namespace string) {
	if cr.Spec.KubeconfigSecretRef != nil && cr.Spec.KubeconfigSecretRef.Name != "" {
		ns := cr.Spec.KubeconfigSecretRef.Namespace
		if ns == "" {
			ns = controllerNamespace
		}
		return cr.Spec.KubeconfigSecretRef.Name, ns
	}
	return cr.Spec.Cluster, controllerNamespace
}

func credentialFailure(src ClusterSource, name string) string {
	diag, ok := src.(interface{ CredentialFailure(string) string })
	if !ok {
		return ""
	}
	return diag.CredentialFailure(name)
}

func (r *CompositionRolloutReconciler) writeCredentialPause(ctx context.Context, cr *rolloutsv1alpha1.CompositionRollout, message string) (ctrl.Result, error) {
	cr.Status.ObservedGeneration = cr.Generation
	for i := range cr.Spec.Compositions {
		st := findCompositionState(&cr.Status, cr.Spec.Compositions[i].Name)
		if st.Phase != rolloutsv1alpha1.PhasePaused {
			prev := st.Phase
			if prev == "" {
				prev = rolloutsv1alpha1.PhaseIdle
			}
			st.PhaseBeforePause = prev
		}
		st.Phase = rolloutsv1alpha1.PhasePaused
		st.PauseReason = &rolloutsv1alpha1.PauseReason{
			Reason:  rolloutsv1alpha1.PauseReasonCredentialUnavailable,
			Message: message,
		}
	}
	updateRolloutConditions(cr)
	if err := r.Status().Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}
	if r.Recorder != nil {
		r.Recorder.Eventf(cr, "Warning", "RolloutPaused", "%s", message)
	}
	return ctrl.Result{RequeueAfter: periodicRequeue}, nil
}

// clearCredentialUnavailable restores the phase once the Secret is engaged.
// A denied patch stays paused until Resume.
func clearCredentialUnavailable(cr *rolloutsv1alpha1.CompositionRollout) {
	for i := range cr.Status.Rollouts {
		st := &cr.Status.Rollouts[i]
		if st.PauseReason == nil || st.PauseReason.Reason != rolloutsv1alpha1.PauseReasonCredentialUnavailable {
			continue
		}
		phase := st.PhaseBeforePause
		if phase == "" {
			phase = rolloutsv1alpha1.PhaseIdle
		}
		st.Phase = phase
		st.PhaseBeforePause = ""
		st.PauseReason = nil
	}
}

func pauseReasonForPatch(err error) string {
	if credentialDenied(err) {
		return rolloutsv1alpha1.PauseReasonCredentialDenied
	}
	return rolloutsv1alpha1.PauseReasonPatchFailed
}

func credentialDenied(err error) bool {
	return apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err)
}

func (r *CompositionRolloutReconciler) noteCredentialDenied(
	cc clusterClients,
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	err error,
) bool {
	if !credentialDenied(err) {
		return false
	}
	already := state.Phase == rolloutsv1alpha1.PhasePaused &&
		state.PauseReason != nil &&
		state.PauseReason.Reason == rolloutsv1alpha1.PauseReasonCredentialDenied
	if state.Phase != rolloutsv1alpha1.PhasePaused {
		prev := state.Phase
		if prev == "" {
			prev = rolloutsv1alpha1.PhaseIdle
		}
		state.PhaseBeforePause = prev
	}
	state.Phase = rolloutsv1alpha1.PhasePaused
	state.PauseReason = &rolloutsv1alpha1.PauseReason{
		Reason:  rolloutsv1alpha1.PauseReasonCredentialDenied,
		Message: err.Error(),
	}
	if !already {
		metrics.IncPause(cc.cluster, cr.Name, target.Name, rolloutsv1alpha1.PauseReasonCredentialDenied, r.MetricsAggregateOnly)
		if cc.recorder != nil {
			cc.recorder.Eventf(cr, "Warning", "RolloutPaused", "composition %s paused: %s", target.Name, err.Error())
		}
	}
	return true
}
