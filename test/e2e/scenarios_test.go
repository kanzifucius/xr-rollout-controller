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

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

var scenarioCases = []struct {
	name string
	run  func()
}{
	{"label selector canary updates only matching XRs", labelSelectorCanary},
	{"label selector with no matches pauses CanarySetEmpty", labelSelectorEmpty},
	{"count canary is stable across a controller restart", countCanaryStable},
	{"percent canary is stable across a controller restart", percentCanaryStable},
	{"soak passes only while Ready and Synced stay True", soakPasses},
	{"a Ready or Synced flap during soak pauses", soakFlapPauses},
	{"dev auto-promotes after soak", devAutoPromotes},
	{"prod waits for Approve", prodAwaitsApproval},
	{"a bad canary pauses without rollback then resume and abort", badCanaryResumeAbort},
	{"an SSA conflict pauses with PatchFailed", ssaConflictPauses},
	{"a second rollout claiming the same composition is rejected", overlapClaimRejected},
	{"a Manual to Automatic flip leaves that XR unmanaged", driftPolicyFlip},
	{"an XR deleted mid-rollout is dropped from the fleet", driftXRDeleted},
	{"an XR created mid-rollout is not added to the canary set", driftXRCreated},
	{"deleting the target revision pauses with TargetRevisionGone", driftTargetRevisionGone},
}

var _ = Describe("preventative scenarios", Ordered, Label("scenarios"), func() {
	BeforeAll(installScenarioCluster)
	AfterAll(teardownScenarioCluster)

	AfterEach(func() {
		defer restoreScenarioCluster()
		if CurrentSpecReport().Failed() {
			dumpScenarioFailure()
		}
	})

	for _, tc := range scenarioCases {
		It(tc.name, tc.run)
	}
})

func restoreScenarioCluster() {
	scaleCrossplane(1)
	setControllerEnvironment("dev")
	cleanupActiveFleet()
}

func labelSelectorCanary() {
	f := seedFleet("label-selector-canary", []xrSpec{
		{name: "a", canary: true},
		{name: "b", canary: true},
		{name: "c"},
		{name: "d"},
	})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector)})
	st := waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	Expect(selectedKeys(st)).To(Equal(xrKeys(f.ns, "a", "b")))
	assertRevisionWrite(f, "a", f.target, before)
	assertRevisionWrite(f, "b", f.target, before)
	assertUnchangedXR(f, "c", before)
	assertUnchangedXR(f, "d", before)
}

func labelSelectorEmpty() {
	f := seedFleet("label-selector-empty", []xrSpec{{name: "a"}, {name: "b"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{
		strategy:  string(rolloutsv1alpha1.CanaryStrategyLabelSelector),
		matchNone: true,
	})
	waitPaused(f, rolloutsv1alpha1.PauseReasonCanarySetEmpty)
	assertUnchangedXR(f, "a", before)
	assertUnchangedXR(f, "b", before)
}

func countCanaryStable() {
	canaryStableAcrossRestart("count-canary-restart", rolloutSpec{
		strategy: string(rolloutsv1alpha1.CanaryStrategyCount),
		count:    1,
		soak:     "20m",
	}, 1)
}

func percentCanaryStable() {
	canaryStableAcrossRestart("percent-canary-restart", rolloutSpec{
		strategy: string(rolloutsv1alpha1.CanaryStrategyPercent),
		percent:  50,
		soak:     "20m",
	}, 2)
}

func canaryStableAcrossRestart(scenario string, spec rolloutSpec, want int) {
	f := seedFleet(scenario, []xrSpec{{name: "a"}, {name: "b"}, {name: "c"}, {name: "d"}})
	applyRollout(f, spec)
	st := waitState(f, 3*time.Minute, func(st rolloutsv1alpha1.CompositionRolloutState) bool {
		return st.Phase == rolloutsv1alpha1.PhaseSoaking && len(st.Canary.Selected) == want
	})
	frozen := selectedKeys(st)
	restartController()
	Consistently(func(g Gomega) {
		cr, err := getRollout(f.rollout)
		g.Expect(err).NotTo(HaveOccurred())
		got, ok := findState(cr, f.composition)
		g.Expect(ok).To(BeTrue())
		g.Expect(selectedKeys(got)).To(Equal(frozen))
	}).WithTimeout(30 * time.Second).WithPolling(3 * time.Second).Should(Succeed())
}

func soakPasses() {
	f := seedFleet("soak-passes", []xrSpec{{name: "a", canary: true}, {name: "b"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector), soak: "3s"})
	waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	assertRevisionWrite(f, "a", f.target, before)
	assertUnchangedXR(f, "b", before)
}

func soakFlapPauses() {
	f := seedFleet("soak-flap", []xrSpec{{name: "a", canary: true}, {name: "b"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{
		strategy:      string(rolloutsv1alpha1.CanaryStrategyLabelSelector),
		soak:          "5m",
		stabilisation: "10m",
	})
	waitPhase(f, rolloutsv1alpha1.PhaseSoaking)
	scaleCrossplane(0)
	setXRConditions(f.ns, "a", "False")
	st := waitPaused(f, rolloutsv1alpha1.PauseReasonCanaryUnhealthy)
	Expect(st.PhaseBeforePause).To(Equal(rolloutsv1alpha1.PhaseSoaking))
	assertRevisionWrite(f, "a", f.target, before)
	assertUnchangedXR(f, "b", before)
}

func devAutoPromotes() {
	f := seedFleet("dev-auto-promote", []xrSpec{{name: "a"}, {name: "b"}, {name: "c"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{
		strategy: string(rolloutsv1alpha1.CanaryStrategyCount),
		count:    1,
		soak:     "3s",
		waveSoak: "2s",
		dev:      string(rolloutsv1alpha1.PromotionAutoPromote),
	})
	waitFinished(f)
	for _, name := range f.xrs {
		assertRevisionWrite(f, name, f.target, before)
	}
}

func prodAwaitsApproval() {
	setControllerEnvironment("prod")
	f := seedFleet("prod-approval", []xrSpec{{name: "a"}, {name: "b"}, {name: "c"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{
		strategy: string(rolloutsv1alpha1.CanaryStrategyCount),
		count:    1,
		soak:     "3s",
		dev:      string(rolloutsv1alpha1.PromotionAutoPromote),
		prod:     string(rolloutsv1alpha1.PromotionManualApproval),
	})
	st := waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	Expect(st.Canary.Selected).To(HaveLen(1))
	for _, name := range f.xrs {
		key := f.ns + "/" + name
		if contains(selectedKeys(st), key) {
			assertRevisionWrite(f, name, f.target, before)
			continue
		}
		assertUnchangedXR(f, name, before)
	}
	addTrigger(f.rollout, f.composition, string(rolloutsv1alpha1.TriggerApprove), st.TargetRevision.Name)
	waitFinished(f)
	for _, name := range f.xrs {
		assertRevisionWrite(f, name, f.target, before)
	}
}

func badCanaryResumeAbort() {
	f := seedFleet("bad-canary", []xrSpec{{name: "a", canary: true}, {name: "b"}, {name: "c"}})
	scaleCrossplane(0)
	setXRConditions(f.ns, "a", "False")
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector), soak: "3s"})
	st := waitPaused(f, rolloutsv1alpha1.PauseReasonCanaryUnhealthy)
	Expect(st.PhaseBeforePause).To(Equal(rolloutsv1alpha1.PhaseCanarying))
	Expect(st.TargetRevision.Name).To(Equal(f.target))
	assertRevisionWrite(f, "a", f.target, before)
	assertUnchangedXR(f, "b", before)
	assertUnchangedXR(f, "c", before)

	setXRConditions(f.ns, "a", "True")
	addTrigger(f.rollout, f.composition, string(rolloutsv1alpha1.TriggerResume), f.target)
	resumed := waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	Expect(resumed.TargetRevision.Name).To(Equal(f.target))

	addTrigger(f.rollout, f.composition, string(rolloutsv1alpha1.TriggerAbort), f.target)
	aborted := waitState(f, 3*time.Minute, func(st rolloutsv1alpha1.CompositionRolloutState) bool {
		return st.Phase == rolloutsv1alpha1.PhaseIdle && st.AbortedRevision == f.target
	})
	Expect(aborted.AbortedRevision).To(Equal(f.target))
	assertRevisionWrite(f, "a", f.target, before)
	assertUnchangedXR(f, "b", before)
	assertUnchangedXR(f, "c", before)
}

func ssaConflictPauses() {
	f := seedFleet("ssa-conflict", []xrSpec{{name: "a", canary: true}, {name: "b"}})
	ownRevision(f.ns, "a", f.source)
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector)})
	waitPaused(f, rolloutsv1alpha1.PauseReasonPatchFailed)
	assertUnchangedXR(f, "a", before)
	assertUnchangedXR(f, "b", before)
}

func overlapClaimRejected() {
	// The admission policy cannot see sibling rollouts, so rejection is the
	// reconciler pausing both objects before any XR write.
	f := seedFleet("overlap-claim", []xrSpec{{name: "a"}, {name: "b"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{
		strategy: string(rolloutsv1alpha1.CanaryStrategyCount),
		count:    1,
		suspend:  true,
	})
	other := f.scenario + "-other"
	applyRollout(f, rolloutSpec{
		strategy:    string(rolloutsv1alpha1.CanaryStrategyCount),
		count:       1,
		suspend:     true,
		rolloutName: other,
	})
	unsuspend(f.rollout)
	unsuspend(other)
	Eventually(func(g Gomega) {
		for _, name := range []string{f.rollout, other} {
			cr, err := getRollout(name)
			g.Expect(err).NotTo(HaveOccurred())
			st, ok := findState(cr, f.composition)
			g.Expect(ok).To(BeTrue(), name)
			g.Expect(st.Phase).To(Equal(rolloutsv1alpha1.PhasePaused), name)
			g.Expect(pauseReason(st)).To(Equal(rolloutsv1alpha1.PauseReasonOverlap), name)
		}
	}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	assertUnchangedXR(f, "a", before)
	assertUnchangedXR(f, "b", before)
}

func driftPolicyFlip() {
	f := seedFleet("drift-policy", []xrSpec{{name: "canary", canary: true}, {name: "stay"}, {name: "flip"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector), soak: "3s"})
	st := waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	runOrFail("kubectl", "patch", xrResource, "flip", "-n", f.ns, "--type=json",
		"-p", `[{"op":"replace","path":"/spec/crossplane/compositionUpdatePolicy","value":"Automatic"}]`)
	Eventually(func(g Gomega) {
		cr, err := getRollout(f.rollout)
		g.Expect(err).NotTo(HaveOccurred())
		got, ok := findState(cr, f.composition)
		g.Expect(ok).To(BeTrue())
		g.Expect(got.UnmanagedXRs).To(BeNumerically(">=", 1))
		g.Expect(policyOf(mustXR(f.ns, "flip"))).To(Equal("Automatic"))
	}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	addTrigger(f.rollout, f.composition, string(rolloutsv1alpha1.TriggerApprove), st.TargetRevision.Name)
	Eventually(func(g Gomega) {
		raw, err := getXR(f.ns, "stay")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(revisionRef(raw)).To(Equal(f.target))
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	assertRevisionWrite(f, "canary", f.target, before)
	assertRevisionWrite(f, "stay", f.target, before)
	Expect(policyOf(mustXR(f.ns, "flip"))).To(Equal("Automatic"))
	assertControllerFields(mustXR(f.ns, "flip"), false)
}

func driftXRDeleted() {
	f := seedFleet("drift-deleted", []xrSpec{{name: "canary", canary: true}, {name: "stay"}, {name: "gone"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector), soak: "3s"})
	waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	runOrFail("kubectl", "delete", xrResource, "gone", "-n", f.ns, "--wait=true")
	st := waitState(f, 2*time.Minute, func(st rolloutsv1alpha1.CompositionRolloutState) bool {
		return st.Phase == rolloutsv1alpha1.PhaseAwaitingApproval && st.TotalXRs == 2
	})
	Expect(st.TotalXRs).To(Equal(int32(2)))
	assertRevisionWrite(f, "canary", f.target, before)
	assertUnchangedXR(f, "stay", before)
}

func driftXRCreated() {
	f := seedFleet("drift-created", []xrSpec{{name: "canary", canary: true}, {name: "stay"}})
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector), soak: "3s"})
	st := waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	frozen := selectedKeys(st)
	scaleCrossplane(0)
	createPinnedXR(f, "extra", f.source)
	Expect(revisionRef(mustXR(f.ns, "extra"))).To(Equal(f.source))
	Expect(frozen).NotTo(ContainElement(f.ns + "/extra"))
	addTrigger(f.rollout, f.composition, string(rolloutsv1alpha1.TriggerApprove), st.TargetRevision.Name)
	Eventually(func(g Gomega) {
		raw, err := getXR(f.ns, "extra")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(revisionRef(raw)).To(Equal(f.target))
		cr, err := getRollout(f.rollout)
		g.Expect(err).NotTo(HaveOccurred())
		got, ok := findState(cr, f.composition)
		g.Expect(ok).To(BeTrue())
		g.Expect(selectedKeys(got)).To(Equal(frozen))
	}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	assertControllerFields(mustXR(f.ns, "extra"), true)
}

func driftTargetRevisionGone() {
	f := seedFleet("drift-revision-gone", []xrSpec{{name: "canary", canary: true}, {name: "stay"}})
	before := snapshotFleet(f)
	applyRollout(f, rolloutSpec{strategy: string(rolloutsv1alpha1.CanaryStrategyLabelSelector), soak: "3s"})
	waitPhase(f, rolloutsv1alpha1.PhaseAwaitingApproval)
	scaleCrossplane(0)
	deleteRevision(f.target)
	waitPaused(f, rolloutsv1alpha1.PauseReasonTargetRevisionGone)
	assertRevisionWrite(f, "canary", f.target, before)
	assertUnchangedXR(f, "stay", before)
}

func unsuspend(name string) {
	runOrFail("kubectl", "patch", rolloutResource, name, "--type=json",
		"-p", `[{"op":"replace","path":"/spec/suspend","value":false}]`)
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
