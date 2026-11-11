package rollout

import (
	"testing"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestTransitionSuspend(t *testing.T) {
	t.Parallel()
	out := Transition(Input{
		Phase: rolloutsv1alpha1.PhaseCanarying,
		Suspend: true,
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
