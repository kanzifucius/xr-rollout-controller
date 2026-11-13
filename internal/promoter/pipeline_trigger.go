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

package promoter

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
)

func matchingSkip(pipeline *rolloutsv1alpha1.PromotionPipeline, cluster, rolloutName, composition string) (string, bool) {
	trigger, ok := findTrigger(pipeline, cluster, rolloutName, composition, "", rolloutsv1alpha1.PromotionTriggerSkip)
	if !ok || trigger.Revision == "" {
		return "", false
	}
	return trigger.Revision, true
}

func approvePresent(pipeline *rolloutsv1alpha1.PromotionPipeline, cluster, rolloutName, composition, revision string) bool {
	_, ok := findTrigger(pipeline, cluster, rolloutName, composition, revision, rolloutsv1alpha1.PromotionTriggerApprove)
	return ok
}

func findTrigger(pipeline *rolloutsv1alpha1.PromotionPipeline, cluster, rolloutName, composition, revision string, action rolloutsv1alpha1.PromotionTriggerAction) (rolloutsv1alpha1.PromotionTrigger, bool) {
	if pipeline == nil {
		return rolloutsv1alpha1.PromotionTrigger{}, false
	}
	for i := range pipeline.Spec.Triggers {
		trigger := pipeline.Spec.Triggers[i]
		if triggerMatches(trigger, cluster, rolloutName, composition, revision, action) {
			return trigger, true
		}
	}
	return rolloutsv1alpha1.PromotionTrigger{}, false
}

func triggerMatches(trigger rolloutsv1alpha1.PromotionTrigger, cluster, rolloutName, composition, revision string, action rolloutsv1alpha1.PromotionTriggerAction) bool {
	if trigger.Cluster != cluster || trigger.Action != action {
		return false
	}
	switch action {
	case rolloutsv1alpha1.PromotionTriggerApprove, rolloutsv1alpha1.PromotionTriggerSkip:
		if trigger.Revision == "" || (revision != "" && trigger.Revision != revision) {
			return false
		}
	default:
		_ = impossiblePromotionTriggerAction(action)
		return false
	}
	if trigger.Rollout != "" && trigger.Rollout != rolloutName {
		return false
	}
	if trigger.Composition != "" && trigger.Composition != composition {
		return false
	}
	return true
}

func acknowledgeUsed(pipeline *rolloutsv1alpha1.PromotionPipeline, from, to, rolloutName, composition, revision string, decision Decision, now time.Time) {
	if pipeline == nil {
		return
	}
	if decision.UsedSkip {
		ackAction(pipeline, from, rolloutName, composition, revision, rolloutsv1alpha1.PromotionTriggerSkip, now)
	}
	if decision.UsedApproval {
		ackAction(pipeline, to, rolloutName, composition, revision, rolloutsv1alpha1.PromotionTriggerApprove, now)
	}
}

func ackAction(pipeline *rolloutsv1alpha1.PromotionPipeline, cluster, rolloutName, composition, revision string, action rolloutsv1alpha1.PromotionTriggerAction, now time.Time) {
	trigger, ok := findTrigger(pipeline, cluster, rolloutName, composition, revision, action)
	if !ok {
		return
	}
	for i := range pipeline.Status.AcknowledgedTriggers {
		existing := pipeline.Status.AcknowledgedTriggers[i]
		if existing.Cluster == trigger.Cluster && existing.Action == trigger.Action && existing.Revision == trigger.Revision && existing.Rollout == trigger.Rollout && existing.Composition == trigger.Composition {
			return
		}
	}
	acked := metav1.NewTime(now)
	pipeline.Status.AcknowledgedTriggers = append(pipeline.Status.AcknowledgedTriggers, rolloutsv1alpha1.AcknowledgedPromotionTrigger{
		Cluster:        trigger.Cluster,
		Action:         trigger.Action,
		Revision:       trigger.Revision,
		Rollout:        trigger.Rollout,
		Composition:    trigger.Composition,
		Outcome:        rollout.TriggerOutcomeApplied,
		AcknowledgedAt: &acked,
	})
}

//nolint:unparam // the result exists only to force exhaustive switch handling
func impossiblePromotionTriggerAction(action rolloutsv1alpha1.PromotionTriggerAction) never {
	switch action {
	case rolloutsv1alpha1.PromotionTriggerApprove, rolloutsv1alpha1.PromotionTriggerSkip:
	default:
		var n never
		return n
	}
	var n never
	return n
}
