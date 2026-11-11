package rollout

import rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"

const (
	TriggerOutcomeApplied = "Applied"
	TriggerOutcomeStale   = "Stale"
)

// FindPendingTrigger returns the first trigger in triggers that applies to composition
// and has not yet been acknowledged.
func FindPendingTrigger(
	triggers []rolloutsv1alpha1.RolloutTrigger,
	ack []rolloutsv1alpha1.AcknowledgedTrigger,
	composition string,
) *rolloutsv1alpha1.RolloutTrigger {
	for i := range triggers {
		t := &triggers[i]
		if t.Composition != composition {
			continue
		}
		if IsAcknowledged(ack, t.Action, t.Revision) {
			continue
		}
		return t
	}
	return nil
}

// IsAcknowledged reports whether action+revision was already consumed.
func IsAcknowledged(ack []rolloutsv1alpha1.AcknowledgedTrigger, action rolloutsv1alpha1.TriggerAction, revision string) bool {
	for i := range ack {
		a := &ack[i]
		if a.Action != action {
			continue
		}
		if a.Revision != revision {
			continue
		}
		return true
	}
	return false
}

// TriggerMatchesRevision reports whether the trigger's revision constraint matches revision.
// Start may omit revision (matches any / latest).
func TriggerMatchesRevision(t *rolloutsv1alpha1.RolloutTrigger, revision string) bool {
	if t == nil {
		return false
	}
	if t.Action == rolloutsv1alpha1.TriggerStart {
		return t.Revision == "" || t.Revision == revision
	}
	return t.Revision == revision
}
