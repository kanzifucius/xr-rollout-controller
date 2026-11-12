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
	"fmt"
	"time"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func predecessorRevision(from ClusterView, policy HopPolicy) (string, bool, Decision, bool) {
	if viewIsDryRun(from) {
		if policy.DryRunChain && from.Spec.DryRunEnabled() {
			if name, ok := simulatedRevision(from); ok {
				return name, true, Decision{}, true
			}
		}
		d := dryRunBlocked("predecessor rollout is a dry run")
		if from.HasState {
			d.Revision = from.State.TargetRevision.Name
		}
		return "", false, d, false
	}
	if from.HasState && from.State.Phase == rolloutsv1alpha1.PhasePaused {
		if from.State.PauseReason != nil && from.State.PauseReason.Reason == rolloutsv1alpha1.PauseReasonSuspended {
			return "", false, Decision{Action: ActionBlocked, Reason: ReasonSuspended}, false
		}
		return "", false, Decision{Action: ActionBlocked, Reason: ReasonPredecessorPaused}, false
	}
	if predecessorAborted(from) {
		return "", false, Decision{Action: ActionBlocked, Reason: ReasonPredecessorAborted}, false
	}
	name, ok := completedRevision(from)
	if !ok {
		return "", false, Decision{}, false
	}
	return name, false, Decision{}, true
}

func simulatedRevision(v ClusterView) (string, bool) {
	if !v.Spec.DryRunEnabled() || !v.HasState {
		return "", false
	}
	name := v.State.TargetRevision.Name
	if name == "" || v.State.AbortedRevision == name {
		return "", false
	}
	if v.State.DryRun == nil || v.State.DryRun.CompletedAt == nil {
		return "", false
	}
	return name, true
}

func completionTime(from ClusterView, simulated bool) time.Time {
	if !from.HasState {
		return time.Time{}
	}
	if simulated {
		if from.State.DryRun != nil && from.State.DryRun.CompletedAt != nil {
			return from.State.DryRun.CompletedAt.Time
		}
		return time.Time{}
	}
	if from.State.CompletedAt != nil {
		return from.State.CompletedAt.Time
	}
	return time.Time{}
}

func holdForRevision(to ClusterView, rev string, policy HopPolicy) (Decision, bool) {
	if policy.RevisionMatch {
		if to.RevisionPresent && to.LatestRevisionName == rev {
			return Decision{}, false
		}
		return revisionMismatch(rev, to.LatestRevisionName), true
	}
	if !to.RevisionPresent {
		return Decision{Action: ActionWait, Reason: ReasonRevisionNotPresent, Revision: rev}, true
	}
	if to.LatestRevisionName != rev {
		return Decision{
			Action:   ActionBlocked,
			Reason:   ReasonRevisionNotLatest,
			Revision: rev,
			Message:  fmt.Sprintf("latest on the next cluster is %s", to.LatestRevisionName),
		}, true
	}
	return Decision{}, false
}

func revisionMismatch(rev, latest string) Decision {
	on := latest
	if on == "" {
		on = "absent"
	}
	return Decision{
		Action:   ActionWait,
		Reason:   ReasonRevisionMismatch,
		Revision: rev,
		Message:  fmt.Sprintf("next cluster composition is on %s, completed revision is %s", on, rev),
	}
}

func applyWriteGuards(d Decision, policy HopPolicy) Decision {
	if d.Action != ActionWriteStart {
		return d
	}
	if next, stop := holdForFleet(d, policy); stop {
		return next
	}
	if next, stop := holdForApproval(d, policy); stop {
		return next
	}
	d.UsedApproval = approvalAccepted(policy)
	if next, stop := holdForSoak(d, policy); stop {
		return next
	}
	if policy.RecordDryRun {
		return recordDryRunStart(d)
	}
	return d
}

func holdForFleet(d Decision, policy HopPolicy) (Decision, bool) {
	if !policy.FleetEnabled || fleetSatisfied(policy.FleetMatching, policy.FleetCompleted, policy.MinCompletedPercent) {
		return Decision{}, false
	}
	d.Action = ActionWait
	d.Reason = ReasonFleetIncomplete
	d.Trigger = rolloutsv1alpha1.RolloutTrigger{}
	d.Message = fmt.Sprintf("%d of %d matching CompositionRollouts have completed %s", policy.FleetCompleted, policy.FleetMatching, d.Revision)
	return d, true
}

func fleetSatisfied(matching, completed int, percent int32) bool {
	if matching <= 0 || percent < 1 || percent > 100 {
		return false
	}
	need := (int(percent)*matching + 99) / 100
	return completed >= need
}

func holdForApproval(d Decision, policy HopPolicy) (Decision, bool) {
	switch policy.Approval {
	case "", rolloutsv1alpha1.PromotionApprovalApprove:
		if policy.Approval != rolloutsv1alpha1.PromotionApprovalApprove || policy.Approved {
			return Decision{}, false
		}
		d.Action = ActionWait
		d.Reason = ReasonApprovalRequired
		d.Trigger = rolloutsv1alpha1.RolloutTrigger{}
		d.Message = "an Approve trigger is required before Start is written"
		return d, true
	default:
		_ = impossiblePromotionApproval(policy.Approval)
		return Decision{}, false
	}
}

func approvalAccepted(policy HopPolicy) bool {
	return policy.Approval == rolloutsv1alpha1.PromotionApprovalApprove && policy.Approved
}

func holdForSoak(d Decision, policy HopPolicy) (Decision, bool) {
	if policy.Soak <= 0 || policy.CompletedAt.IsZero() {
		return Decision{}, false
	}
	ready := policy.CompletedAt.Add(policy.Soak)
	if !policy.Now.Before(ready) {
		return Decision{}, false
	}
	d.Action = ActionWait
	d.Reason = ReasonInterClusterSoak
	d.Trigger = rolloutsv1alpha1.RolloutTrigger{}
	d.Message = fmt.Sprintf("inter-cluster soak until %s", ready.UTC().Format(time.RFC3339))
	d.RequeueAfter = ready.Sub(policy.Now)
	return d, true
}

func recordDryRunStart(d Decision) Decision {
	d.Action = ActionRecorded
	d.Reason = ReasonDryRunRecorded
	d.Message = "predecessor rollout is a dry run; Start was recorded and not written"
	return d
}

//nolint:unparam // the result exists only to force exhaustive switch handling
func impossiblePromotionApproval(approval rolloutsv1alpha1.PromotionApproval) never {
	switch approval {
	case "", rolloutsv1alpha1.PromotionApprovalApprove:
	default:
		var n never
		return n
	}
	var n never
	return n
}
