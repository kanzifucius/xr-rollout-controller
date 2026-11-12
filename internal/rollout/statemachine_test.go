package rollout

import (
	"testing"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestTransitionSuspend(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:              rolloutsv1alpha1.PhaseCanarying,
		Suspend:            true,
		TargetRevisionName: "comp-v2",
	})
	if out.Phase != rolloutsv1alpha1.PhasePaused {
		t.Fatalf("phase = %s", out.Phase)
	}
	if out.PauseReason == nil || out.PauseReason.Reason != rolloutsv1alpha1.PauseReasonSuspended {
		t.Fatalf("pause reason = %#v", out.PauseReason)
	}
	if out.PhaseBeforePause != rolloutsv1alpha1.PhaseCanarying {
		t.Fatalf("phase before pause = %s", out.PhaseBeforePause)
	}
}

func TestTransitionIdleAutoStart(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:                rolloutsv1alpha1.PhaseIdle,
		AutoStart:            true,
		LatestRevisionName:   "comp-v3",
		LatestRevisionNumber: 3,
		TargetRevisionNumber: 2,
		TargetRevisionName:   "comp-v2",
	})
	if !out.StartRollout || out.Phase != rolloutsv1alpha1.PhaseCanarying {
		t.Fatalf("out = %#v", out)
	}
}

func TestTransitionIdleAutoStartSkipsAborted(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:                rolloutsv1alpha1.PhaseIdle,
		AutoStart:            true,
		LatestRevisionName:   "comp-v3",
		LatestRevisionNumber: 3,
		TargetRevisionNumber: 2,
		AbortedRevision:      "comp-v3",
	})
	if out.StartRollout || out.Phase != rolloutsv1alpha1.PhaseIdle {
		t.Fatalf("out = %#v", out)
	}
}

func TestTransitionStartTrigger(t *testing.T) {
	t.Parallel()
	trigger := &rolloutsv1alpha1.RolloutTrigger{
		Action: rolloutsv1alpha1.TriggerStart,
	}
	out := Transition(Input{
		Phase:              rolloutsv1alpha1.PhaseIdle,
		LatestRevisionName: "comp-v2",
		Trigger:            trigger,
	})
	if out.TriggerOutcome != TriggerOutcomeApplied || !out.StartRollout {
		t.Fatalf("out = %#v", out)
	}
}

func TestTransitionStartTriggerStaleRevision(t *testing.T) {
	t.Parallel()
	trigger := &rolloutsv1alpha1.RolloutTrigger{
		Action:   rolloutsv1alpha1.TriggerStart,
		Revision: "comp-v1",
	}
	out := Transition(Input{
		Phase:              rolloutsv1alpha1.PhaseIdle,
		LatestRevisionName: "comp-v2",
		Trigger:            trigger,
	})
	if out.TriggerOutcome != TriggerOutcomeStale || out.StartRollout {
		t.Fatalf("out = %#v", out)
	}
}

func TestTransitionCanaryingToSoaking(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:            rolloutsv1alpha1.PhaseCanarying,
		CanaryAllPatched: true,
	})
	if out.Phase != rolloutsv1alpha1.PhaseSoaking || !out.EnterSoak {
		t.Fatalf("out = %#v", out)
	}
}

func TestTransitionCanaryFailed(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:        rolloutsv1alpha1.PhaseCanarying,
		CanaryFailed: true,
	})
	if out.Phase != rolloutsv1alpha1.PhasePaused {
		t.Fatalf("out = %#v", out)
	}
	out = Transition(Input{
		Phase:        rolloutsv1alpha1.PhaseSoaking,
		CanaryFailed: true,
	})
	if out.Phase != rolloutsv1alpha1.PhasePaused || out.PauseReason.Reason != rolloutsv1alpha1.PauseReasonStabilisationTimeout {
		t.Fatalf("out = %#v", out)
	}
}

func TestTransitionSoakPassedPolicies(t *testing.T) {
	t.Parallel()
	auto := Transition(Input{
		Phase:           rolloutsv1alpha1.PhaseSoaking,
		SoakPassed:      true,
		PromotionPolicy: rolloutsv1alpha1.PromotionAutoPromote,
	})
	if auto.Phase != rolloutsv1alpha1.PhasePromoting || !auto.EnterPromoting {
		t.Fatalf("auto = %#v", auto)
	}
	manual := Transition(Input{
		Phase:           rolloutsv1alpha1.PhaseSoaking,
		SoakPassed:      true,
		PromotionPolicy: rolloutsv1alpha1.PromotionManualApproval,
	})
	if manual.Phase != rolloutsv1alpha1.PhaseAwaitingApproval {
		t.Fatalf("manual = %#v", manual)
	}
}

func TestTransitionApproveAbortResume(t *testing.T) {
	t.Parallel()
	target := "comp-v4"
	approve := Transition(Input{
		Phase:              rolloutsv1alpha1.PhaseAwaitingApproval,
		TargetRevisionName: target,
		Trigger: &rolloutsv1alpha1.RolloutTrigger{
			Action:   rolloutsv1alpha1.TriggerApprove,
			Revision: target,
		},
	})
	if approve.Phase != rolloutsv1alpha1.PhasePromoting || !approve.EnterPromoting {
		t.Fatalf("approve = %#v", approve)
	}

	staleApprove := Transition(Input{
		Phase:              rolloutsv1alpha1.PhaseAwaitingApproval,
		TargetRevisionName: target,
		Trigger: &rolloutsv1alpha1.RolloutTrigger{
			Action:   rolloutsv1alpha1.TriggerApprove,
			Revision: "comp-v3",
		},
	})
	if staleApprove.TriggerOutcome != TriggerOutcomeStale {
		t.Fatalf("stale approve = %#v", staleApprove)
	}

	abort := Transition(Input{
		Phase:              rolloutsv1alpha1.PhaseAwaitingApproval,
		TargetRevisionName: target,
		Trigger: &rolloutsv1alpha1.RolloutTrigger{
			Action:   rolloutsv1alpha1.TriggerAbort,
			Revision: target,
		},
	})
	if abort.Phase != rolloutsv1alpha1.PhaseAborted || !abort.MarkAborted {
		t.Fatalf("abort = %#v", abort)
	}

	resume := Transition(Input{
		Phase:              rolloutsv1alpha1.PhasePaused,
		TargetRevisionName: target,
		PhaseBeforePause:   rolloutsv1alpha1.PhaseSoaking,
		Trigger: &rolloutsv1alpha1.RolloutTrigger{
			Action:   rolloutsv1alpha1.TriggerResume,
			Revision: target,
		},
	})
	if resume.Phase != rolloutsv1alpha1.PhaseSoaking || resume.TriggerOutcome != TriggerOutcomeApplied {
		t.Fatalf("resume = %#v", resume)
	}

	pausedAbort := Transition(Input{
		Phase:              rolloutsv1alpha1.PhasePaused,
		TargetRevisionName: target,
		Trigger: &rolloutsv1alpha1.RolloutTrigger{
			Action:   rolloutsv1alpha1.TriggerAbort,
			Revision: target,
		},
	})
	if pausedAbort.Phase != rolloutsv1alpha1.PhaseAborted {
		t.Fatalf("paused abort = %#v", pausedAbort)
	}
}

func TestTransitionPromotingCompleteOrFail(t *testing.T) {
	t.Parallel()
	done := Transition(Input{
		Phase:         rolloutsv1alpha1.PhasePromoting,
		WavesComplete: true,
	})
	if done.Phase != rolloutsv1alpha1.PhaseCompleted || !done.MarkCompleted {
		t.Fatalf("done = %#v", done)
	}
	fail := Transition(Input{
		Phase:      rolloutsv1alpha1.PhasePromoting,
		WaveFailed: true,
	})
	if fail.Phase != rolloutsv1alpha1.PhasePaused || fail.PauseReason.Reason != rolloutsv1alpha1.PauseReasonWaveUnhealthy {
		t.Fatalf("fail = %#v", fail)
	}
}

func TestTransitionSupersede(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:                rolloutsv1alpha1.PhaseSoaking,
		OnNewRevision:        rolloutsv1alpha1.NewRevisionRestart,
		LatestRevisionName:   "comp-v5",
		LatestRevisionNumber: 5,
		TargetRevisionName:   "comp-v4",
		TargetRevisionNumber: 4,
	})
	if !out.Supersede || !out.StartRollout || out.Phase != rolloutsv1alpha1.PhaseCanarying {
		t.Fatalf("out = %#v", out)
	}
	queued := Transition(Input{
		Phase:                rolloutsv1alpha1.PhaseSoaking,
		OnNewRevision:        rolloutsv1alpha1.NewRevisionQueue,
		LatestRevisionName:   "comp-v5",
		LatestRevisionNumber: 5,
		TargetRevisionName:   "comp-v4",
		TargetRevisionNumber: 4,
	})
	if queued.Supersede || queued.Phase != rolloutsv1alpha1.PhaseSoaking {
		t.Fatalf("queued = %#v", queued)
	}
}

func TestTransitionSoakFailedPauses(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:      rolloutsv1alpha1.PhaseSoaking,
		SoakFailed: true,
	})
	if out.Phase != rolloutsv1alpha1.PhasePaused || out.PauseReason == nil {
		t.Fatalf("out = %#v", out)
	}
	if out.PauseReason.Reason != rolloutsv1alpha1.PauseReasonCanaryUnhealthy {
		t.Fatalf("out = %#v", out)
	}
	if out.PhaseBeforePause != rolloutsv1alpha1.PhaseSoaking {
		t.Fatalf("phaseBeforePause = %s", out.PhaseBeforePause)
	}
}

func TestTransitionTargetRevisionMissing(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase:                 rolloutsv1alpha1.PhaseAwaitingApproval,
		TargetRevisionMissing: true,
		TargetRevisionName:    "comp-v2",
	})
	if out.Phase != rolloutsv1alpha1.PhasePaused || out.PauseReason == nil {
		t.Fatalf("out = %#v", out)
	}
	if out.PauseReason.Reason != rolloutsv1alpha1.PauseReasonTargetRevisionGone {
		t.Fatalf("reason = %s", out.PauseReason.Reason)
	}

	idle := Transition(Input{
		Phase:                 rolloutsv1alpha1.PhaseIdle,
		TargetRevisionMissing: true,
	})
	if idle.Phase != rolloutsv1alpha1.PhaseIdle {
		t.Fatalf("idle = %#v", idle)
	}

	abort := Transition(Input{
		Phase:                 rolloutsv1alpha1.PhasePaused,
		TargetRevisionMissing: true,
		TargetRevisionName:    "comp-v2",
		Trigger: &rolloutsv1alpha1.RolloutTrigger{
			Action:   rolloutsv1alpha1.TriggerAbort,
			Revision: "comp-v2",
		},
	})
	if abort.Phase != rolloutsv1alpha1.PhaseAborted || abort.TriggerOutcome != TriggerOutcomeApplied {
		t.Fatalf("abort = %#v", abort)
	}
}

func TestTransitionTerminalToIdle(t *testing.T) {
	t.Parallel()
	aborted := Transition(Input{Phase: rolloutsv1alpha1.PhaseAborted})
	if aborted.Phase != rolloutsv1alpha1.PhaseIdle {
		t.Fatalf("aborted = %#v", aborted)
	}
	completed := Transition(Input{Phase: rolloutsv1alpha1.PhaseCompleted})
	if completed.Phase != rolloutsv1alpha1.PhaseIdle {
		t.Fatalf("completed = %#v", completed)
	}
}

func TestStartOutcomeMatchesApply(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      Input
		applied bool
		outcome string
	}{
		{
			name: "idle latest with explicit revision",
			in: Input{
				Phase:              rolloutsv1alpha1.PhaseIdle,
				LatestRevisionName: "comp-v2",
				Trigger: &rolloutsv1alpha1.RolloutTrigger{
					Action:   rolloutsv1alpha1.TriggerStart,
					Revision: "comp-v2",
				},
			},
			applied: true,
			outcome: TriggerOutcomeApplied,
		},
		{
			name: "named revision is not latest",
			in: Input{
				Phase:              rolloutsv1alpha1.PhaseIdle,
				LatestRevisionName: "comp-v2",
				Trigger: &rolloutsv1alpha1.RolloutTrigger{
					Action:   rolloutsv1alpha1.TriggerStart,
					Revision: "comp-v1",
				},
			},
			outcome: TriggerOutcomeStale,
		},
		{
			name: "not idle",
			in: Input{
				Phase:              rolloutsv1alpha1.PhaseCanarying,
				LatestRevisionName: "comp-v2",
				Trigger: &rolloutsv1alpha1.RolloutTrigger{
					Action:   rolloutsv1alpha1.TriggerStart,
					Revision: "comp-v2",
				},
			},
			outcome: TriggerOutcomeStale,
		},
		{
			name: "latest is the aborted revision",
			in: Input{
				Phase:              rolloutsv1alpha1.PhaseIdle,
				LatestRevisionName: "comp-v2",
				AbortedRevision:    "comp-v2",
				Trigger: &rolloutsv1alpha1.RolloutTrigger{
					Action:   rolloutsv1alpha1.TriggerStart,
					Revision: "comp-v2",
				},
			},
			outcome: TriggerOutcomeStale,
		},
		{
			name: "approve is not a start",
			in: Input{
				Phase: rolloutsv1alpha1.PhaseIdle,
				Trigger: &rolloutsv1alpha1.RolloutTrigger{
					Action: rolloutsv1alpha1.TriggerApprove,
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			applied, outcome := StartOutcome(tc.in)
			if applied != tc.applied || outcome != tc.outcome {
				t.Fatalf("StartOutcome = (%v, %q), want (%v, %q)", applied, outcome, tc.applied, tc.outcome)
			}
		})
	}
}
