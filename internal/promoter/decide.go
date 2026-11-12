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

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
)

const (
	ActionNone       = "None"
	ActionWriteStart = "WriteStart"
	ActionWait       = "Wait"
	ActionBlocked    = "Blocked"
	ActionRecorded   = "Recorded"

	ReasonNoCompletedRevision    = "NoCompletedRevision"
	ReasonPredecessorPaused      = "PredecessorPaused"
	ReasonPredecessorAborted     = "PredecessorAborted"
	ReasonSuspended              = "Suspended"
	ReasonClusterNotEngaged      = "ClusterNotEngaged"
	ReasonRolloutNotFound        = "RolloutNotFound"
	ReasonCompositionNotGoverned = "CompositionNotGoverned"
	ReasonRevisionNotPresent     = "RevisionNotPresent"
	ReasonRevisionNotLatest      = "RevisionNotLatest"
	ReasonDestinationSuspended   = "DestinationSuspended"
	ReasonDestinationPaused      = "DestinationPaused"
	ReasonDestinationAborted     = "DestinationAborted"
	ReasonDestinationNotIdle     = "DestinationNotIdle"
	ReasonTriggerStale           = "TriggerStale"
	ReasonAlreadyWritten         = "AlreadyWritten"
	ReasonAlreadyAcknowledged    = "AlreadyAcknowledged"
	ReasonAlreadyStarted         = "AlreadyStarted"
	ReasonStartWriteFailed       = "StartWriteFailed"

	// Actor is the audit name written on a promoter Start trigger.
	Actor = "xr-rollout-promoter"
)

// ClusterView is one cluster's CompositionRollout, as the promoter sees it.
type ClusterView struct {
	Engaged            bool
	Found              bool
	Spec               rolloutsv1alpha1.CompositionRolloutSpec
	State              rolloutsv1alpha1.CompositionRolloutState
	HasState           bool
	LatestRevisionName string
	RevisionPresent    bool
}

// Decision is whether the next cluster should receive a Start.
type Decision struct {
	Action   string
	Reason   string
	Revision string
	Message  string
	Trigger  rolloutsv1alpha1.RolloutTrigger
}

// DecideHop reports what to do for one adjacent pair and one composition.
func DecideHop(from, to ClusterView, composition string) Decision {
	if !from.Engaged || !to.Engaged {
		return Decision{Action: ActionWait, Reason: ReasonClusterNotEngaged}
	}
	if !from.Found {
		return Decision{Action: ActionWait, Reason: ReasonRolloutNotFound}
	}
	if from.HasState && from.State.Phase == rolloutsv1alpha1.PhasePaused {
		if from.State.PauseReason != nil && from.State.PauseReason.Reason == rolloutsv1alpha1.PauseReasonSuspended {
			return Decision{Action: ActionBlocked, Reason: ReasonSuspended}
		}
		return Decision{Action: ActionBlocked, Reason: ReasonPredecessorPaused}
	}
	if predecessorAborted(from) {
		return Decision{Action: ActionBlocked, Reason: ReasonPredecessorAborted}
	}
	rev, ok := completedRevision(from)
	if !ok {
		return Decision{Action: ActionWait, Reason: ReasonNoCompletedRevision}
	}
	d := Decision{Revision: rev}
	if !to.Found {
		d.Action = ActionWait
		d.Reason = ReasonRolloutNotFound
		return d
	}
	if !governs(to.Spec, composition) {
		d.Action = ActionBlocked
		d.Reason = ReasonCompositionNotGoverned
		return d
	}
	if to.Spec.Suspend {
		d.Action = ActionWait
		d.Reason = ReasonDestinationSuspended
		return d
	}
	if !to.RevisionPresent {
		d.Action = ActionWait
		d.Reason = ReasonRevisionNotPresent
		return d
	}
	if to.LatestRevisionName != rev {
		d.Action = ActionBlocked
		d.Reason = ReasonRevisionNotLatest
		d.Message = fmt.Sprintf("latest on the next cluster is %s", to.LatestRevisionName)
		return d
	}
	switch ackOutcome(to.State, rev) {
	case rollout.TriggerOutcomeStale:
		d.Action = ActionBlocked
		d.Reason = ReasonTriggerStale
		return d
	case rollout.TriggerOutcomeApplied:
		d.Action = ActionRecorded
		d.Reason = ReasonAlreadyAcknowledged
		return d
	}
	if specHasStart(to.Spec, composition, rev) {
		d.Action = ActionRecorded
		d.Reason = ReasonAlreadyWritten
		return d
	}
	phase := to.State.Phase
	if !to.HasState || phase == "" {
		phase = rolloutsv1alpha1.PhaseIdle
	}
	// A paused destination has not accepted the revision. Check that before
	// treating any non-idle phase on this revision as already started.
	if phase == rolloutsv1alpha1.PhasePaused {
		d.Action = ActionBlocked
		d.Reason = ReasonDestinationPaused
		return d
	}
	if phase != rolloutsv1alpha1.PhaseIdle && to.State.TargetRevision.Name == rev && to.State.AbortedRevision != rev {
		d.Action = ActionRecorded
		d.Reason = ReasonAlreadyStarted
		if to.Spec.AutoStartEnabled() {
			d.Message = "autoStart is true on the next cluster, so the gate was not held"
		}
		return d
	}
	if to.State.AbortedRevision == rev {
		d.Action = ActionBlocked
		d.Reason = ReasonDestinationAborted
		return d
	}
	trigger := rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerStart,
		Revision:    rev,
		Actor:       Actor,
	}
	applied, outcome := rollout.StartOutcome(rollout.Input{
		Phase:              phase,
		LatestRevisionName: to.LatestRevisionName,
		AbortedRevision:    to.State.AbortedRevision,
		Trigger:            &trigger,
	})
	if applied && outcome == rollout.TriggerOutcomeApplied {
		d.Action = ActionWriteStart
		d.Trigger = trigger
		if to.Spec.AutoStartEnabled() {
			d.Message = "autoStart is true on the next cluster; Start is still written, but the gate is not held for the next revision"
		}
		return d
	}
	if phase != rolloutsv1alpha1.PhaseIdle {
		d.Action = ActionWait
		d.Reason = ReasonDestinationNotIdle
		return d
	}
	d.Action = ActionBlocked
	d.Reason = ReasonTriggerStale
	return d
}

func completedRevision(v ClusterView) (string, bool) {
	if !v.HasState {
		return "", false
	}
	name := v.State.TargetRevision.Name
	if name == "" || v.State.AbortedRevision == name {
		return "", false
	}
	switch v.State.Phase {
	case rolloutsv1alpha1.PhaseCompleted:
		return name, true
	case rolloutsv1alpha1.PhaseIdle:
		if v.State.CompletedAt != nil {
			return name, true
		}
	case rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePromoting,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseAborted:
		return "", false
	default:
		var n never
		_ = n
		return "", false
	}
	return "", false
}

type never = struct{}

func predecessorAborted(v ClusterView) bool {
	if !v.HasState {
		return false
	}
	if v.State.Phase == rolloutsv1alpha1.PhaseAborted {
		return true
	}
	return v.State.AbortedRevision != "" && v.State.AbortedRevision == v.State.TargetRevision.Name
}

func governs(spec rolloutsv1alpha1.CompositionRolloutSpec, composition string) bool {
	for i := range spec.Compositions {
		if spec.Compositions[i].Name == composition {
			return true
		}
	}
	return false
}

func specHasStart(spec rolloutsv1alpha1.CompositionRolloutSpec, composition, revision string) bool {
	for i := range spec.Triggers {
		t := spec.Triggers[i]
		if t.Composition == composition && t.Action == rolloutsv1alpha1.TriggerStart && t.Revision == revision {
			return true
		}
	}
	return false
}

func ackOutcome(state rolloutsv1alpha1.CompositionRolloutState, revision string) string {
	for i := range state.AcknowledgedTriggers {
		a := state.AcknowledgedTriggers[i]
		if a.Action == rolloutsv1alpha1.TriggerStart && a.Revision == revision {
			return a.Outcome
		}
	}
	return ""
}
