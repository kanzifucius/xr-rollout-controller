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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setCondition(conds *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i := range *conds {
		if (*conds)[i].Type == condType {
			(*conds)[i].Status = status
			(*conds)[i].Reason = reason
			(*conds)[i].Message = message
			(*conds)[i].LastTransitionTime = now
			return
		}
	}
	*conds = append(*conds, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}

func updateRolloutConditions(cr *rolloutsv1alpha1.CompositionRollout) {
	progressing := false
	awaiting := false
	paused := false
	succeeded := false

	for i := range cr.Status.Rollouts {
		st := &cr.Status.Rollouts[i]
		switch st.Phase {
		case rolloutsv1alpha1.PhaseCanarying,
			rolloutsv1alpha1.PhaseSoaking,
			rolloutsv1alpha1.PhasePromoting:
			progressing = true
		case rolloutsv1alpha1.PhaseAwaitingApproval:
			progressing = true
			awaiting = true
		case rolloutsv1alpha1.PhasePaused:
			paused = true
		case rolloutsv1alpha1.PhaseCompleted:
			succeeded = true
		case rolloutsv1alpha1.PhaseIdle,
			rolloutsv1alpha1.PhaseAborted:
		default:
			_ = impossiblePhase(st.Phase)
		}
	}

	conds := cr.Status.Conditions
	if progressing && !paused {
		setCondition(&conds, rolloutsv1alpha1.ConditionProgressing, metav1.ConditionTrue, "Active", "One or more composition rollouts are in progress")
	} else {
		setCondition(&conds, rolloutsv1alpha1.ConditionProgressing, metav1.ConditionFalse, "Idle", "No active rollout work")
	}
	if awaiting {
		setCondition(&conds, rolloutsv1alpha1.ConditionAwaitingApproval, metav1.ConditionTrue, "ApprovalRequired", "Promotion awaiting manual approval")
	} else {
		setCondition(&conds, rolloutsv1alpha1.ConditionAwaitingApproval, metav1.ConditionFalse, "NotWaiting", "")
	}
	if paused || cr.Spec.Suspend {
		setCondition(&conds, rolloutsv1alpha1.ConditionPaused, metav1.ConditionTrue, "Paused", "Rollout paused")
	} else {
		setCondition(&conds, rolloutsv1alpha1.ConditionPaused, metav1.ConditionFalse, "Running", "")
	}
	if succeeded {
		setCondition(&conds, rolloutsv1alpha1.ConditionSucceeded, metav1.ConditionTrue, "Completed", "At least one composition rollout completed")
	} else {
		setCondition(&conds, rolloutsv1alpha1.ConditionSucceeded, metav1.ConditionFalse, "Incomplete", "")
	}
	cr.Status.Conditions = conds
}

type neverPhase struct{}

func impossiblePhase(ph rolloutsv1alpha1.RolloutPhase) neverPhase {
	switch ph {
	case rolloutsv1alpha1.PhaseIdle,
		rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePromoting,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseCompleted,
		rolloutsv1alpha1.PhaseAborted:
	default:
		var n neverPhase
		return n
	}
	var n neverPhase
	return n
}
