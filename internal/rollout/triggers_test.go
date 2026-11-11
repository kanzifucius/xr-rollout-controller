package rollout

import (
	"testing"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestIsAcknowledged(t *testing.T) {
	t.Parallel()
	ack := []rolloutsv1alpha1.AcknowledgedTrigger{
		{Action: rolloutsv1alpha1.TriggerStart, Revision: "comp-v1", Outcome: TriggerOutcomeApplied},
		{Action: rolloutsv1alpha1.TriggerApprove, Revision: "comp-v3", Outcome: TriggerOutcomeApplied},
	}

	if !IsAcknowledged(ack, rolloutsv1alpha1.TriggerStart, "comp-v1") {
		t.Fatal("expected start v1 acknowledged")
	}
	if IsAcknowledged(ack, rolloutsv1alpha1.TriggerStart, "comp-v2") {
		t.Fatal("expected start v2 not acknowledged")
	}
	if IsAcknowledged(ack, rolloutsv1alpha1.TriggerResume, "comp-v1") {
		t.Fatal("expected resume not acknowledged")
	}
}

func TestTriggerMatchesRevision(t *testing.T) {
	t.Parallel()
	startEmpty := &rolloutsv1alpha1.RolloutTrigger{Action: rolloutsv1alpha1.TriggerStart}
	if !TriggerMatchesRevision(startEmpty, "comp-v5") {
		t.Fatal("start without revision should match any")
	}
	startV3 := &rolloutsv1alpha1.RolloutTrigger{Action: rolloutsv1alpha1.TriggerStart, Revision: "comp-v3"}
	if !TriggerMatchesRevision(startV3, "comp-v3") {
		t.Fatal("start with revision should match")
	}
	if TriggerMatchesRevision(startV3, "comp-v4") {
		t.Fatal("start with revision should not match other revision")
	}
	approve := &rolloutsv1alpha1.RolloutTrigger{Action: rolloutsv1alpha1.TriggerApprove, Revision: "comp-v3"}
	if !TriggerMatchesRevision(approve, "comp-v3") {
		t.Fatal("approve must match exact revision")
	}
	if TriggerMatchesRevision(approve, "") {
		t.Fatal("approve requires revision")
	}
}

func TestFindPendingTrigger(t *testing.T) {
	t.Parallel()
	triggers := []rolloutsv1alpha1.RolloutTrigger{
		{Composition: "other", Action: rolloutsv1alpha1.TriggerStart},
		{Composition: "comp", Action: rolloutsv1alpha1.TriggerStart, Revision: "comp-v1"},
		{Composition: "comp", Action: rolloutsv1alpha1.TriggerApprove, Revision: "comp-v2"},
	}
	ack := []rolloutsv1alpha1.AcknowledgedTrigger{
		{Action: rolloutsv1alpha1.TriggerStart, Revision: "comp-v1", Outcome: TriggerOutcomeApplied},
	}

	pending := FindPendingTrigger(triggers, ack, "comp")
	if pending == nil || pending.Action != rolloutsv1alpha1.TriggerApprove {
		t.Fatalf("expected approve trigger, got %#v", pending)
	}

	if FindPendingTrigger(nil, nil, "comp") != nil {
		t.Fatal("expected nil when no triggers")
	}
}
