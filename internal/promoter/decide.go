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
	ReasonDryRun                 = "DryRun"
	ReasonApprovalRequired       = "ApprovalRequired"
	ReasonRevisionMismatch       = "RevisionMismatch"
	ReasonInterClusterSoak       = "InterClusterSoak"
	ReasonFleetIncomplete        = "FleetIncomplete"
	ReasonDryRunRecorded         = "DryRunRecorded"

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
	// Labels are the CompositionRollout labels. Fleet selectors use them.
	Labels map[string]string
}

// HopPolicy is the opt-in gate for one hop. The zero value keeps DecideHop.
type HopPolicy struct {
	Now                 time.Time
	Approval            rolloutsv1alpha1.PromotionApproval
	Approved            bool
	RevisionMatch       bool
	Soak                time.Duration
	FleetEnabled        bool
	MinCompletedPercent int32
	FleetMatching       int
	FleetCompleted      int
	SkipRevision        string
	DryRunChain         bool
	RecordDryRun        bool
	CompletedAt         time.Time
}

// Decision is whether the next cluster should receive a Start.
type Decision struct {
	Action       string
	Reason       string
	Revision     string
	Message      string
	Trigger      rolloutsv1alpha1.RolloutTrigger
	RequeueAfter time.Duration
	UsedSkip     bool
	UsedApproval bool
}

func viewIsDryRun(v ClusterView) bool {
	if v.Spec.DryRunEnabled() {
		return true
	}
	return v.HasState && v.State.DryRun != nil && v.State.DryRun.Active
}

func dryRunBlocked(message string) Decision {
	return Decision{Action: ActionBlocked, Reason: ReasonDryRun, Message: message}
}

// DecideHop reports what to do for one adjacent pair and one composition.
// A zero HopPolicy is the default gate.
func DecideHop(from, to ClusterView, composition string) Decision {
	return DecideHopPolicy(from, to, composition, HopPolicy{})
}

// DecideHopPolicy applies one hop's optional gates. Unset policy fields
// keep DecideHop's answers.
func DecideHopPolicy(from, to ClusterView, composition string, policy HopPolicy) Decision {
	skip := policy.SkipRevision
	if viewIsDryRun(from) {
		skip = ""
	}
	if !to.Engaged || (skip == "" && !from.Engaged) {
		return Decision{Action: ActionWait, Reason: ReasonClusterNotEngaged}
	}
	if skip != "" {
		policy.SkipRevision = skip
		policy.RecordDryRun = false
		policy.CompletedAt = completionTime(from, false)
		d := decideDestination(to, composition, skip, policy)
		d.UsedSkip = true
		return d
	}
	if !from.Found {
		return Decision{Action: ActionWait, Reason: ReasonRolloutNotFound}
	}
	rev, record, early, ok := predecessorRevision(from, policy)
	if !ok {
		if early.Action != "" {
			return early
		}
		return Decision{Action: ActionWait, Reason: ReasonNoCompletedRevision}
	}
	policy.RecordDryRun = record
	policy.CompletedAt = completionTime(from, record)
	return decideDestination(to, composition, rev, policy)
}

func decideDestination(to ClusterView, composition, rev string, policy HopPolicy) Decision {
	d := Decision{Revision: rev}
	if !to.Found {
		d.Action = ActionWait
		d.Reason = ReasonRolloutNotFound
		return d
	}
	if viewIsDryRun(to) {
		d.Action = ActionBlocked
		d.Reason = ReasonDryRun
		d.Message = "next cluster rollout is a dry run"
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
	if held, ok := holdForRevision(to, rev, policy); ok {
		return held
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
	return applyWriteGuards(decideStart(to, composition, rev, d), policy)
}

func decideStart(to ClusterView, composition, rev string, d Decision) Decision {
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
	// Idle after the Completed blink still has completedAt. Writing Start
	// would run the revision again.
	if phase == rolloutsv1alpha1.PhaseIdle && to.HasState && to.State.CompletedAt != nil &&
		to.State.TargetRevision.Name == rev && to.State.AbortedRevision != rev {
		d.Action = ActionRecorded
		d.Reason = ReasonAlreadyStarted
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
