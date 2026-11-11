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
	"errors"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/metrics"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

const compositionRevisionRefField = "spec.crossplane.compositionRevisionRef.name"

// ssaConflictManager matches the apiserver apply-conflict text, for example
// `conflict with "argocd"` or `conflicts with "argocd"`.
var ssaConflictManager = regexp.MustCompile(`conflicts? with "([^"]+)"`)

// revisionRefApplyConfiguration is the server-side apply body for one XR.
// It carries only the identity of the object and the single field this
// controller is allowed to own.
func revisionRefApplyConfiguration(src *unstructured.Unstructured, revName string) *unstructured.Unstructured {
	apply := &unstructured.Unstructured{}
	apply.SetAPIVersion(src.GetAPIVersion())
	apply.SetKind(src.GetKind())
	apply.SetName(src.GetName())
	if ns := src.GetNamespace(); ns != "" {
		apply.SetNamespace(ns)
	}
	xp.SetCompositionRevisionRef(apply, revName)
	return apply
}

func (r *CompositionRolloutReconciler) fieldManager() string {
	if r.FieldManager != "" {
		return r.FieldManager
	}
	return defaultFieldManager
}

func (r *CompositionRolloutReconciler) patchXRRevision(ctx context.Context, u *unstructured.Unstructured, revName string) error {
	applyCfg := revisionRefApplyConfiguration(u, revName)
	opts := []client.PatchOption{client.FieldOwner(r.fieldManager())}
	if r.ForceOwnership {
		opts = append(opts, client.ForceOwnership)
	}
	return r.Patch(ctx, applyCfg, client.Apply, opts...)
}

// ssaConflictingManagers reports whether err is a persistent server-side apply
// field-manager conflict, and the managers that own the field. Optimistic
// concurrency conflicts are not included; those are transient and should retry.
func ssaConflictingManagers(err error) ([]string, bool) {
	if err == nil || !apierrors.IsConflict(err) {
		return nil, false
	}
	fromStatus, sawCause := managersFromStatus(err)
	fromText := managersFromText(err.Error())
	managers := mergeManagers(fromStatus, fromText)
	if sawCause || len(fromText) > 0 || strings.Contains(err.Error(), "Apply failed with") {
		return managers, true
	}
	return nil, false
}

func managersFromStatus(err error) ([]string, bool) {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return nil, false
	}
	details := status.Status().Details
	if details == nil {
		return nil, false
	}
	var managers []string
	saw := false
	for _, cause := range details.Causes {
		if cause.Type != metav1.CauseTypeFieldManagerConflict {
			continue
		}
		saw = true
		managers = mergeManagers(managers, managersFromText(cause.Message))
	}
	return managers, saw
}

func managersFromText(text string) []string {
	matches := ssaConflictManager.FindAllStringSubmatch(text, -1)
	managers := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		managers = mergeManagers(managers, []string{match[1]})
	}
	return managers
}

func mergeManagers(dst, src []string) []string {
	for _, manager := range src {
		if manager == "" {
			continue
		}
		seen := false
		for _, existing := range dst {
			if existing == manager {
				seen = true
				break
			}
		}
		if !seen {
			dst = append(dst, manager)
		}
	}
	return dst
}

func patchConflictMessage(managers []string) string {
	if len(managers) == 0 {
		return "server-side apply conflict on " + compositionRevisionRefField
	}
	quoted := make([]string, len(managers))
	for i, manager := range managers {
		quoted[i] = fmt.Sprintf("%q", manager)
	}
	return "server-side apply conflict on " + compositionRevisionRefField + " with field manager " + strings.Join(quoted, ", ")
}

func (r *CompositionRolloutReconciler) pauseForPatchConflict(
	cr *rolloutsv1alpha1.CompositionRollout,
	target rolloutsv1alpha1.CompositionTarget,
	state *rolloutsv1alpha1.CompositionRolloutState,
	managers []string,
) {
	message := patchConflictMessage(managers)
	if state.Phase != rolloutsv1alpha1.PhasePaused {
		state.PhaseBeforePause = rolloutsv1alpha1.PhaseCanarying
		metrics.IncPause(cr.Name, target.Name, rolloutsv1alpha1.PauseReasonPatchFailed, r.MetricsAggregateOnly)
		r.Recorder.Eventf(cr, "Warning", "RolloutPaused", "composition %s paused: %s", target.Name, message)
	}
	state.Phase = rolloutsv1alpha1.PhasePaused
	state.PauseReason = &rolloutsv1alpha1.PauseReason{
		Reason:  rolloutsv1alpha1.PauseReasonPatchFailed,
		Message: message,
	}
}
