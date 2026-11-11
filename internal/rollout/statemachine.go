package rollout

import rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"

// Input is a snapshot of per-composition rollout state fed into Transition.
type Input struct {
	Phase                  rolloutsv1alpha1.RolloutPhase
	Suspend                bool
	AutoStart              bool
	OnNewRevision          rolloutsv1alpha1.NewRevisionPolicy
	PromotionPolicy        rolloutsv1alpha1.PromotionPolicy
	LatestRevisionName     string
	LatestRevisionNumber   int64
	TargetRevisionName     string
	TargetRevisionNumber   int64
	AbortedRevision        string
	CanaryAllPatched       bool
	CanaryHealthy          bool
	CanaryFailed           bool
	SoakPassed             bool
	WavesComplete          bool
	WaveFailed             bool
	Trigger                *rolloutsv1alpha1.RolloutTrigger
	PhaseBeforePause       rolloutsv1alpha1.RolloutPhase
}

// Output is the pure state-machine decision for one reconcile step.
type Output struct {
	Phase              rolloutsv1alpha1.RolloutPhase
	PauseReason        *rolloutsv1alpha1.PauseReason
	PhaseBeforePause   rolloutsv1alpha1.RolloutPhase
	AcknowledgeTrigger bool
	TriggerOutcome     string
	StartRollout       bool
	ClearAbort         bool
	MarkAborted        bool
	MarkCompleted      bool
	EnterSoak          bool
	EnterPromoting     bool
	Supersede          bool
}

// Transition computes the next rollout phase and side effects from Input.
func Transition(in Input) Output {
	out := Output{
		Phase:            in.Phase,
		PhaseBeforePause: in.PhaseBeforePause,
	}

	if in.Phase == rolloutsv1alpha1.PhaseAborted || in.Phase == rolloutsv1alpha1.PhaseCompleted {
		out.Phase = rolloutsv1alpha1.PhaseIdle
		if in.Phase == rolloutsv1alpha1.PhaseCompleted {
			out.MarkCompleted = false
		}
		return out
	}

	if in.Suspend {
		return suspendOutput(in)
	}

	if in.Trigger != nil {
		if handled, trigOut := applyTrigger(in); handled {
			return trigOut
		}
	}

	if newerRevisionPending(in) && in.OnNewRevision == rolloutsv1alpha1.NewRevisionRestart && isMidRollout(in.Phase) {
		return supersedeOutput(in)
	}

	switch in.Phase {
	case rolloutsv1alpha1.PhaseIdle:
		return idleProgress(in, out)
	case rolloutsv1alpha1.PhaseCanarying:
		return canaryingProgress(in, out)
	case rolloutsv1alpha1.PhaseSoaking:
		return soakingProgress(in, out)
	case rolloutsv1alpha1.PhaseAwaitingApproval:
		return out
	case rolloutsv1alpha1.PhasePromoting:
		return promotingProgress(in, out)
	case rolloutsv1alpha1.PhasePaused:
		return out
	case rolloutsv1alpha1.PhaseCompleted, rolloutsv1alpha1.PhaseAborted:
		return out
	default:
		return impossiblePhase(in.Phase, out)
	}
}

type never = struct{}

func suspendOutput(in Input) Output {
	phaseBefore := in.PhaseBeforePause
	if in.Phase != rolloutsv1alpha1.PhasePaused {
		phaseBefore = in.Phase
	}
	return Output{
		Phase:            rolloutsv1alpha1.PhasePaused,
		PauseReason:      pauseReasonSuspended(),
		PhaseBeforePause: phaseBefore,
	}
}

func impossiblePhase(phase rolloutsv1alpha1.RolloutPhase, out Output) Output {
	switch phase {
	case rolloutsv1alpha1.PhaseIdle,
		rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePromoting,
		rolloutsv1alpha1.PhasePaused,
		rolloutsv1alpha1.PhaseCompleted,
		rolloutsv1alpha1.PhaseAborted:
		return out
	default:
		var n never
		_ = n
		return out
	}
}

func pauseReasonSuspended() *rolloutsv1alpha1.PauseReason {
	return &rolloutsv1alpha1.PauseReason{
		Reason: rolloutsv1alpha1.PauseReasonSuspended,
	}
}

func pauseReasonCanaryUnhealthy() *rolloutsv1alpha1.PauseReason {
	return &rolloutsv1alpha1.PauseReason{
		Reason: rolloutsv1alpha1.PauseReasonCanaryUnhealthy,
	}
}

func pauseReasonStabilisationTimeout() *rolloutsv1alpha1.PauseReason {
	return &rolloutsv1alpha1.PauseReason{
		Reason: rolloutsv1alpha1.PauseReasonStabilisationTimeout,
	}
}

func pauseReasonWaveUnhealthy() *rolloutsv1alpha1.PauseReason {
	return &rolloutsv1alpha1.PauseReason{
		Reason: rolloutsv1alpha1.PauseReasonWaveUnhealthy,
	}
}

func isMidRollout(phase rolloutsv1alpha1.RolloutPhase) bool {
	switch phase {
	case rolloutsv1alpha1.PhaseCanarying,
		rolloutsv1alpha1.PhaseSoaking,
		rolloutsv1alpha1.PhaseAwaitingApproval,
		rolloutsv1alpha1.PhasePromoting:
		return true
	default:
		return false
	}
}

func newerRevisionPending(in Input) bool {
	if in.LatestRevisionName == "" {
		return false
	}
	if in.LatestRevisionName == in.AbortedRevision {
		return false
	}
	if in.TargetRevisionNumber == 0 {
		return in.LatestRevisionNumber > 0
	}
	return in.LatestRevisionNumber > in.TargetRevisionNumber
}

func supersedeOutput(in Input) Output {
	return Output{
		Phase:        rolloutsv1alpha1.PhaseCanarying,
		StartRollout: true,
		Supersede:    true,
	}
}

func applyTrigger(in Input) (bool, Output) {
	t := in.Trigger
	out := Output{
		Phase:            in.Phase,
		PhaseBeforePause: in.PhaseBeforePause,
	}

	switch t.Action {
	case rolloutsv1alpha1.TriggerStart:
		return applyStartTrigger(in, out)
	case rolloutsv1alpha1.TriggerApprove:
		return applyApproveTrigger(in, out)
	case rolloutsv1alpha1.TriggerResume:
		return applyResumeTrigger(in, out)
	case rolloutsv1alpha1.TriggerAbort:
		return applyAbortTrigger(in, out)
	default:
		_ = impossibleTriggerAction(t.Action)
		return false, out
	}
}

func impossibleTriggerAction(action rolloutsv1alpha1.TriggerAction) never {
	switch action {
	case rolloutsv1alpha1.TriggerStart,
		rolloutsv1alpha1.TriggerApprove,
		rolloutsv1alpha1.TriggerResume,
		rolloutsv1alpha1.TriggerAbort:
	default:
		var n never
		return n
	}
	var n never
	return n
}

func applyStartTrigger(in Input, out Output) (bool, Output) {
	if in.Phase != rolloutsv1alpha1.PhaseIdle {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	if in.LatestRevisionName == "" || in.LatestRevisionName == in.AbortedRevision {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	if in.Trigger.Revision != "" && in.Trigger.Revision != in.LatestRevisionName {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	out.AcknowledgeTrigger = true
	out.TriggerOutcome = TriggerOutcomeApplied
	out.Phase = rolloutsv1alpha1.PhaseCanarying
	out.StartRollout = true
	out.ClearAbort = true
	return true, out
}

func applyApproveTrigger(in Input, out Output) (bool, Output) {
	if in.Phase != rolloutsv1alpha1.PhaseAwaitingApproval {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	if !TriggerMatchesRevision(in.Trigger, in.TargetRevisionName) {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	out.AcknowledgeTrigger = true
	out.TriggerOutcome = TriggerOutcomeApplied
	out.Phase = rolloutsv1alpha1.PhasePromoting
	out.EnterPromoting = true
	return true, out
}

func applyResumeTrigger(in Input, out Output) (bool, Output) {
	if in.Phase != rolloutsv1alpha1.PhasePaused {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	if !TriggerMatchesRevision(in.Trigger, in.TargetRevisionName) {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	resumePhase := in.PhaseBeforePause
	if resumePhase == "" {
		resumePhase = rolloutsv1alpha1.PhaseCanarying
	}
	out.AcknowledgeTrigger = true
	out.TriggerOutcome = TriggerOutcomeApplied
	out.Phase = resumePhase
	out.PhaseBeforePause = ""
	out.PauseReason = nil
	return true, out
}

func applyAbortTrigger(in Input, out Output) (bool, Output) {
	switch in.Phase {
	case rolloutsv1alpha1.PhaseAwaitingApproval, rolloutsv1alpha1.PhasePaused:
	default:
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	if !TriggerMatchesRevision(in.Trigger, in.TargetRevisionName) {
		out.AcknowledgeTrigger = true
		out.TriggerOutcome = TriggerOutcomeStale
		return true, out
	}
	out.AcknowledgeTrigger = true
	out.TriggerOutcome = TriggerOutcomeApplied
	out.Phase = rolloutsv1alpha1.PhaseAborted
	out.MarkAborted = true
	return true, out
}

func idleProgress(in Input, out Output) Output {
	if !in.AutoStart || !newerRevisionPending(in) {
		return out
	}
	out.Phase = rolloutsv1alpha1.PhaseCanarying
	out.StartRollout = true
	out.ClearAbort = true
	return out
}

func canaryingProgress(in Input, out Output) Output {
	if in.CanaryFailed {
		out.Phase = rolloutsv1alpha1.PhasePaused
		out.PauseReason = pauseReasonCanaryUnhealthy()
		out.PhaseBeforePause = rolloutsv1alpha1.PhaseCanarying
		return out
	}
	if in.CanaryAllPatched {
		out.Phase = rolloutsv1alpha1.PhaseSoaking
		out.EnterSoak = true
	}
	return out
}

func soakingProgress(in Input, out Output) Output {
	if in.CanaryFailed {
		out.Phase = rolloutsv1alpha1.PhasePaused
		out.PauseReason = pauseReasonStabilisationTimeout()
		out.PhaseBeforePause = rolloutsv1alpha1.PhaseSoaking
		return out
	}
	if !in.SoakPassed {
		return out
	}
	switch in.PromotionPolicy {
	case rolloutsv1alpha1.PromotionAutoPromote:
		out.Phase = rolloutsv1alpha1.PhasePromoting
		out.EnterPromoting = true
	case rolloutsv1alpha1.PromotionManualApproval:
		out.Phase = rolloutsv1alpha1.PhaseAwaitingApproval
	default:
		_ = impossiblePromotionPolicy(in.PromotionPolicy)
		out.Phase = rolloutsv1alpha1.PhaseAwaitingApproval
	}
	return out
}

func impossiblePromotionPolicy(policy rolloutsv1alpha1.PromotionPolicy) never {
	switch policy {
	case rolloutsv1alpha1.PromotionAutoPromote, rolloutsv1alpha1.PromotionManualApproval:
	default:
		var n never
		return n
	}
	var n never
	return n
}

func promotingProgress(in Input, out Output) Output {
	if in.WaveFailed {
		out.Phase = rolloutsv1alpha1.PhasePaused
		out.PauseReason = pauseReasonWaveUnhealthy()
		out.PhaseBeforePause = rolloutsv1alpha1.PhasePromoting
		return out
	}
	if in.WavesComplete {
		out.Phase = rolloutsv1alpha1.PhaseCompleted
		out.MarkCompleted = true
	}
	return out
}
