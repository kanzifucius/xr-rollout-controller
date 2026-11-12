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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
)

func TestDecideHop(t *testing.T) {
	t.Parallel()

	idleDest := readyTo(false)
	notPresent := readyTo(false)
	notPresent.RevisionPresent = false
	notPresent.LatestRevisionName = ""

	newer := readyTo(false)
	newer.LatestRevisionName = nextRevision
	newer.RevisionPresent = true

	ungoverned := readyTo(false)
	ungoverned.Spec.Compositions = nil

	suspended := readyTo(false)
	suspended.Spec.Suspend = true

	alreadyWritten := readyTo(false)
	alreadyWritten.Spec.Triggers = []rolloutsv1alpha1.RolloutTrigger{{
		Composition: testComposition,
		Action:      rolloutsv1alpha1.TriggerStart,
		Revision:    testRevision,
	}}

	ackedApplied := readyTo(false)
	ackedApplied.State.AcknowledgedTriggers = []rolloutsv1alpha1.AcknowledgedTrigger{{
		Action:   rolloutsv1alpha1.TriggerStart,
		Revision: testRevision,
		Outcome:  rollout.TriggerOutcomeApplied,
	}}

	ackedStale := readyTo(false)
	ackedStale.State.AcknowledgedTriggers = []rolloutsv1alpha1.AcknowledgedTrigger{{
		Action:   rolloutsv1alpha1.TriggerStart,
		Revision: testRevision,
		Outcome:  rollout.TriggerOutcomeStale,
	}}

	canaryingThis := readyTo(false)
	canaryingThis.State.Phase = rolloutsv1alpha1.PhaseCanarying
	canaryingThis.State.TargetRevision.Name = testRevision

	canaryingOther := readyTo(false)
	canaryingOther.State.Phase = rolloutsv1alpha1.PhaseCanarying
	canaryingOther.State.TargetRevision.Name = nextRevision

	pausedDest := readyTo(false)
	pausedDest.State.Phase = rolloutsv1alpha1.PhasePaused
	pausedDest.State.TargetRevision.Name = testRevision

	noState := readyTo(false)
	noState.HasState = false
	noState.State = rolloutsv1alpha1.CompositionRolloutState{}

	pausedFrom := completedFrom(rolloutsv1alpha1.PhasePaused)

	suspendedFrom := completedFrom(rolloutsv1alpha1.PhasePaused)
	suspendedFrom.State.PauseReason = &rolloutsv1alpha1.PauseReason{
		Reason: rolloutsv1alpha1.PauseReasonSuspended,
	}

	abortedFrom := completedFrom(rolloutsv1alpha1.PhaseIdle)
	abortedFrom.State.AbortedRevision = testRevision

	cases := []struct {
		name    string
		from    ClusterView
		to      ClusterView
		action  string
		reason  string
		write   bool
		message string
	}{
		{
			name:   "predecessor still canarying",
			from:   completedFrom(rolloutsv1alpha1.PhaseCanarying),
			to:     idleDest,
			action: ActionWait,
			reason: ReasonNoCompletedRevision,
		},
		{
			name:   "predecessor paused",
			from:   pausedFrom,
			to:     idleDest,
			action: ActionBlocked,
			reason: ReasonPredecessorPaused,
		},
		{
			name:   "predecessor suspended",
			from:   suspendedFrom,
			to:     idleDest,
			action: ActionBlocked,
			reason: ReasonSuspended,
		},
		{
			name:   "predecessor aborted",
			from:   abortedFrom,
			to:     idleDest,
			action: ActionBlocked,
			reason: ReasonPredecessorAborted,
		},
		{
			name:   "completed blink",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     idleDest,
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "idle after blink",
			from:   completedFrom(rolloutsv1alpha1.PhaseIdle),
			to:     idleDest,
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "revision not on next cluster",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     notPresent,
			action: ActionWait,
			reason: ReasonRevisionNotPresent,
		},
		{
			name:   "next cluster has a newer latest",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     newer,
			action: ActionBlocked,
			reason: ReasonRevisionNotLatest,
		},
		{
			name:   "next object missing the composition",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     ungoverned,
			action: ActionBlocked,
			reason: ReasonCompositionNotGoverned,
		},
		{
			name:   "next rollout object missing",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     ClusterView{Engaged: true, Found: false},
			action: ActionWait,
			reason: ReasonRolloutNotFound,
		},
		{
			name:   "next cluster not engaged",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     ClusterView{Engaged: false},
			action: ActionWait,
			reason: ReasonClusterNotEngaged,
		},
		{
			name:   "next spec.suspend",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     suspended,
			action: ActionWait,
			reason: ReasonDestinationSuspended,
		},
		{
			name:   "already in spec",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     alreadyWritten,
			action: ActionRecorded,
			reason: ReasonAlreadyWritten,
		},
		{
			name:   "acked applied",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     ackedApplied,
			action: ActionRecorded,
			reason: ReasonAlreadyAcknowledged,
		},
		{
			name:   "acked stale",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     ackedStale,
			action: ActionBlocked,
			reason: ReasonTriggerStale,
		},
		{
			name:   "destination already canarying this revision",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     canaryingThis,
			action: ActionRecorded,
			reason: ReasonAlreadyStarted,
		},
		{
			name:   "destination canarying a different revision",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     canaryingOther,
			action: ActionWait,
			reason: ReasonDestinationNotIdle,
		},
		{
			name:   "destination paused on this revision",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     pausedDest,
			action: ActionBlocked,
			reason: ReasonDestinationPaused,
		},
		{
			name:   "no state yet counts as idle",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     noState,
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "destination already completed this revision",
			from:   completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:     completedDestination(),
			action: ActionRecorded,
			reason: ReasonAlreadyStarted,
		},
		{
			name:    "autoStart on the next cluster still receives Start",
			from:    completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:      readyTo(true),
			action:  ActionWriteStart,
			write:   true,
			message: "autoStart is true on the next cluster; Start is still written, but the gate is not held for the next revision",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := DecideHop(tc.from, tc.to, testComposition)
			if d.Action != tc.action || d.Reason != tc.reason {
				t.Fatalf("DecideHop = %#v, want action %s reason %s", d, tc.action, tc.reason)
			}
			if tc.message != "" && d.Message != tc.message {
				t.Fatalf("message = %q, want %q", d.Message, tc.message)
			}
			if !tc.write {
				return
			}
			if d.Trigger.Action != rolloutsv1alpha1.TriggerStart ||
				d.Trigger.Composition != testComposition ||
				d.Trigger.Revision != testRevision ||
				d.Trigger.Actor != Actor {
				t.Fatalf("trigger = %#v", d.Trigger)
			}
		})
	}
}

func completedFrom(phase rolloutsv1alpha1.RolloutPhase) ClusterView {
	now := metav1.Now()
	return ClusterView{
		Engaged:  true,
		Found:    true,
		HasState: true,
		State: rolloutsv1alpha1.CompositionRolloutState{
			Composition:    testComposition,
			Phase:          phase,
			TargetRevision: rolloutsv1alpha1.RevisionInfo{Name: testRevision, Revision: 7},
			CompletedAt:    &now,
		},
	}
}

func completedDestination() ClusterView {
	to := readyTo(false)
	done := completedFrom(rolloutsv1alpha1.PhaseIdle)
	to.State = done.State
	to.HasState = true
	return to
}

func readyTo(auto bool) ClusterView {
	spec := rolloutsv1alpha1.CompositionRolloutSpec{
		Compositions: []rolloutsv1alpha1.CompositionTarget{{Name: testComposition}},
	}
	if !auto {
		f := false
		spec.AutoStart = &f
	}
	return ClusterView{
		Engaged:            true,
		Found:              true,
		HasState:           true,
		LatestRevisionName: testRevision,
		RevisionPresent:    true,
		Spec:               spec,
		State: rolloutsv1alpha1.CompositionRolloutState{
			Phase: rolloutsv1alpha1.PhaseIdle,
		},
	}
}
