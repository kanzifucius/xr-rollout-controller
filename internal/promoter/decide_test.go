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
	"time"

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
		{
			name:    "predecessor spec dry run",
			from:    dryRunView(completedFrom(rolloutsv1alpha1.PhaseCompleted)),
			to:      idleDest,
			action:  ActionBlocked,
			reason:  ReasonDryRun,
			message: "predecessor rollout is a dry run",
		},
		{
			name: "predecessor dry run still active after the flag was cleared",
			from: func() ClusterView {
				v := completedFrom(rolloutsv1alpha1.PhaseIdle)
				v.State.DryRun = &rolloutsv1alpha1.DryRunStatus{Active: true}
				return v
			}(),
			to:      idleDest,
			action:  ActionBlocked,
			reason:  ReasonDryRun,
			message: "predecessor rollout is a dry run",
		},
		{
			name:    "next cluster spec dry run",
			from:    completedFrom(rolloutsv1alpha1.PhaseCompleted),
			to:      dryRunView(idleDest),
			action:  ActionBlocked,
			reason:  ReasonDryRun,
			message: "next cluster rollout is a dry run",
		},
		{
			name: "simulation completedAt is not a real completion",
			from: func() ClusterView {
				v := completedFrom(rolloutsv1alpha1.PhaseIdle)
				v.State.CompletedAt = nil
				now := metav1.Now()
				v.State.DryRun = &rolloutsv1alpha1.DryRunStatus{CompletedAt: &now}
				return v
			}(),
			to:     idleDest,
			action: ActionWait,
			reason: ReasonNoCompletedRevision,
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

func TestDecideHopPolicyGates(t *testing.T) {
	t.Parallel()
	completedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	from := completedFrom(rolloutsv1alpha1.PhaseCompleted)
	from.State.CompletedAt = &metav1.Time{Time: completedAt}
	idle := readyTo(false)
	newer := readyTo(false)
	newer.LatestRevisionName = nextRevision

	cases := []struct {
		name    string
		from    ClusterView
		to      ClusterView
		policy  HopPolicy
		action  string
		reason  string
		write   bool
		message string
	}{
		{
			name:   "approval required without an Approve trigger",
			from:   from,
			to:     idle,
			policy: HopPolicy{Approval: rolloutsv1alpha1.PromotionApprovalApprove},
			action: ActionWait,
			reason: ReasonApprovalRequired,
		},
		{
			name:   "approval granted writes Start",
			from:   from,
			to:     idle,
			policy: HopPolicy{Approval: rolloutsv1alpha1.PromotionApprovalApprove, Approved: true},
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "revision match holds a different composition revision",
			from:   from,
			to:     newer,
			policy: HopPolicy{RevisionMatch: true},
			action: ActionWait,
			reason: ReasonRevisionMismatch,
		},
		{
			name:   "revision match writes the same revision",
			from:   from,
			to:     idle,
			policy: HopPolicy{RevisionMatch: true},
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "inter-cluster soak waits after completion",
			from:   from,
			to:     idle,
			policy: HopPolicy{Now: completedAt.Add(30 * time.Minute), Soak: time.Hour},
			action: ActionWait,
			reason: ReasonInterClusterSoak,
		},
		{
			name:   "inter-cluster soak elapsed writes Start",
			from:   from,
			to:     idle,
			policy: HopPolicy{Now: completedAt.Add(time.Hour), Soak: time.Hour},
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "partial fleet below the percentage waits",
			from:   from,
			to:     idle,
			policy: HopPolicy{FleetEnabled: true, MinCompletedPercent: 50, FleetMatching: 4, FleetCompleted: 1},
			action: ActionWait,
			reason: ReasonFleetIncomplete,
		},
		{
			name:   "partial fleet at the percentage writes Start",
			from:   from,
			to:     idle,
			policy: HopPolicy{FleetEnabled: true, MinCompletedPercent: 50, FleetMatching: 4, FleetCompleted: 2},
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "unset fleet ignores the counts",
			from:   from,
			to:     idle,
			policy: HopPolicy{FleetMatching: 4, FleetCompleted: 0},
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "skip satisfies a cluster that has not completed",
			from:   completedFrom(rolloutsv1alpha1.PhaseCanarying),
			to:     idle,
			policy: HopPolicy{SkipRevision: testRevision},
			action: ActionWriteStart,
			write:  true,
		},
		{
			name:   "skip does not write when the predecessor is a dry run",
			from:   dryRunView(from),
			to:     idle,
			policy: HopPolicy{SkipRevision: testRevision},
			action: ActionBlocked,
			reason: ReasonDryRun,
		},
		{
			name:   "dry-run chain records Start and does not ask for a write",
			from:   finishedSimulation(),
			to:     idle,
			policy: HopPolicy{DryRunChain: true},
			action: ActionRecorded,
			reason: ReasonDryRunRecorded,
			write:  true,
		},
		{
			name:   "dry-run chain stays blocked until the simulation finishes",
			from:   dryRunView(from),
			to:     idle,
			policy: HopPolicy{DryRunChain: true},
			action: ActionBlocked,
			reason: ReasonDryRun,
		},
		{
			name:   "dry-run chain does not record onto a dry-run destination",
			from:   finishedSimulation(),
			to:     dryRunView(idle),
			policy: HopPolicy{DryRunChain: true},
			action: ActionBlocked,
			reason: ReasonDryRun,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := DecideHopPolicy(tc.from, tc.to, testComposition, tc.policy)
			if d.Action != tc.action || d.Reason != tc.reason {
				t.Fatalf("DecideHopPolicy = %#v, want action %s reason %s", d, tc.action, tc.reason)
			}
			if tc.message != "" && d.Message != tc.message {
				t.Fatalf("message = %q, want %q", d.Message, tc.message)
			}
			if tc.reason == ReasonDryRunRecorded && (d.Trigger.Action != rolloutsv1alpha1.TriggerStart || d.Trigger.Revision != testRevision || d.Trigger.Actor != Actor) {
				t.Fatalf("recorded trigger = %#v", d.Trigger)
			}
			if !tc.write || tc.reason == ReasonDryRunRecorded {
				if tc.reason != ReasonDryRunRecorded && d.Trigger.Action != "" {
					t.Fatalf("trigger = %#v, want empty", d.Trigger)
				}
				return
			}
			if d.Trigger.Action != rolloutsv1alpha1.TriggerStart || d.Trigger.Revision != testRevision || d.Trigger.Actor != Actor {
				t.Fatalf("trigger = %#v", d.Trigger)
			}
		})
	}
}

func TestFleetSatisfiedRoundsUp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		matching, done int
		percent        int32
		want           bool
	}{
		{name: "half of one", matching: 1, done: 1, percent: 50, want: true},
		{name: "half of two needs one", matching: 2, done: 1, percent: 50, want: true},
		{name: "every one of two", matching: 2, done: 1, percent: 100, want: false},
		{name: "half of three needs two", matching: 3, done: 1, percent: 50, want: false},
		{name: "empty fleet", matching: 0, done: 0, percent: 100, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fleetSatisfied(tc.matching, tc.done, tc.percent)
			if got != tc.want {
				t.Fatalf("fleetSatisfied(%d, %d, %d) = %v, want %v", tc.matching, tc.done, tc.percent, got, tc.want)
			}
		})
	}
}

func finishedSimulation() ClusterView {
	done := metav1.NewTime(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	view := dryRunView(completedFrom(rolloutsv1alpha1.PhaseIdle))
	view.State.CompletedAt = nil
	view.State.TargetRevision.Name = testRevision
	view.State.DryRun = &rolloutsv1alpha1.DryRunStatus{Active: true, CompletedAt: &done}
	return view
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

func dryRunView(v ClusterView) ClusterView {
	on := true
	v.Spec.DryRun = &on
	return v
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
