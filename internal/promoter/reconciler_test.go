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
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/rollout"
)

const (
	testRollout     = "platformdb"
	testComposition = "platformdb.example.org"
	testRevision    = "platformdb.example.org-aaa"
	nextRevision    = "platformdb.example.org-bbb"
)

type memWriter struct {
	triggers []rolloutsv1alpha1.RolloutTrigger
	err      error
}

func (w *memWriter) AppendStart(_ context.Context, _ client.Client, _ string, trigger rolloutsv1alpha1.RolloutTrigger) error {
	if w.err != nil {
		return w.err
	}
	w.triggers = append(w.triggers, trigger)
	return nil
}

func TestReconcilePipelineWritesStartOnce(t *testing.T) {
	t.Parallel()
	views := map[string]ClusterView{
		"dev":  completedFrom(rolloutsv1alpha1.PhaseCompleted),
		"test": readyTo(false),
	}
	writer := &memWriter{}
	hops, err := reconcileFixture(t, []string{"dev", "test"}, views, writer)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 1 {
		t.Fatalf("triggers = %#v, want one Start", writer.triggers)
	}
	assertHop(t, hops, "dev", "test", testRevision)
	if writer.triggers[0].Action != rolloutsv1alpha1.TriggerStart || writer.triggers[0].Revision != testRevision {
		t.Fatalf("trigger = %#v", writer.triggers[0])
	}

	written := views["test"]
	written.Spec.Triggers = append(written.Spec.Triggers, writer.triggers[0])
	views["test"] = written
	writer.triggers = nil
	again, err := reconcileFixture(t, []string{"dev", "test"}, views, writer)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("second triggers = %#v, want none", writer.triggers)
	}
	if again[0].Action != ActionRecorded || again[0].Reason != ReasonAlreadyWritten {
		t.Fatalf("second hop = %#v", again[0])
	}
}

func TestReconcilePipelinePredecessorPaused(t *testing.T) {
	t.Parallel()
	writer := &memWriter{}
	hops, err := reconcileFixture(t, []string{"dev", "test"}, map[string]ClusterView{
		"dev":  completedFrom(rolloutsv1alpha1.PhasePaused),
		"test": readyTo(false),
	}, writer)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("triggers = %#v, want none", writer.triggers)
	}
	if hops[0].Action != ActionBlocked || hops[0].Reason != ReasonPredecessorPaused {
		t.Fatalf("hop = %#v", hops[0])
	}
	assertHop(t, hops, "dev", "test", "")
}

func TestReconcilePipelineDryRunDoesNotWrite(t *testing.T) {
	t.Parallel()
	writer := &memWriter{}
	hops, err := reconcileFixture(t, []string{"dev", "test"}, map[string]ClusterView{
		"dev":  dryRunView(completedFrom(rolloutsv1alpha1.PhaseCompleted)),
		"test": readyTo(false),
	}, writer)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("triggers = %#v, want none", writer.triggers)
	}
	if hops[0].Action != ActionBlocked || hops[0].Reason != ReasonDryRun {
		t.Fatalf("hop = %#v", hops[0])
	}

	writer = &memWriter{}
	hops, err = reconcileFixture(t, []string{"dev", "test"}, map[string]ClusterView{
		"dev":  completedFrom(rolloutsv1alpha1.PhaseCompleted),
		"test": dryRunView(readyTo(false)),
	}, writer)
	if err != nil {
		t.Fatalf("destination reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("destination triggers = %#v, want none", writer.triggers)
	}
	if hops[0].Reason != ReasonDryRun || hops[0].Action != ActionBlocked {
		t.Fatalf("destination hop = %#v", hops[0])
	}
}

func TestReconcilePipelinePredecessorAborted(t *testing.T) {
	t.Parallel()
	from := completedFrom(rolloutsv1alpha1.PhaseIdle)
	from.State.AbortedRevision = testRevision
	writer := &memWriter{}
	hops, err := reconcileFixture(t, []string{"dev", "test"}, map[string]ClusterView{
		"dev":  from,
		"test": readyTo(false),
	}, writer)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("triggers = %#v, want none", writer.triggers)
	}
	if hops[0].Action != ActionBlocked || hops[0].Reason != ReasonPredecessorAborted {
		t.Fatalf("hop = %#v", hops[0])
	}
}

func TestReconcilePipelineWriteFailure(t *testing.T) {
	t.Parallel()
	writer := &memWriter{err: fmt.Errorf("apply conflict")}
	hops, err := reconcileFixture(t, []string{"dev", "test"}, map[string]ClusterView{
		"dev":  completedFrom(rolloutsv1alpha1.PhaseCompleted),
		"test": readyTo(false),
	}, writer)
	if err == nil || err.Error() != "apply conflict" {
		t.Fatalf("err = %v, want apply conflict", err)
	}
	if hops[0].Action != ActionWait || hops[0].Reason != ReasonStartWriteFailed {
		t.Fatalf("hop = %#v", hops[0])
	}
}

func TestReconcilePipelineDoesNotSkipAhead(t *testing.T) {
	t.Parallel()
	writer := &memWriter{}
	hops, err := reconcileFixture(t, []string{"dev", "test", "acc", "prod"}, map[string]ClusterView{
		"dev":  completedFrom(rolloutsv1alpha1.PhaseCompleted),
		"test": readyTo(false),
		"acc":  readyTo(false),
		"prod": readyTo(false),
	}, writer)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 1 {
		t.Fatalf("triggers = %#v, want one Start for test", writer.triggers)
	}
	assertHop(t, hops, "dev", "test", testRevision)
	for _, hop := range hops {
		if hop.To == "acc" || hop.To == "prod" {
			if hop.Reason == ReasonAlreadyWritten || hop.Action == ActionWriteStart {
				t.Fatalf("hop %s -> %s was written: %#v", hop.From, hop.To, hop)
			}
		}
	}
}

func TestReconcilePipelineOpensClusterAfterFinished(t *testing.T) {
	t.Parallel()
	writer := &memWriter{}
	hops, err := reconcileFixture(t, []string{"dev", "test", "acc", "prod"}, map[string]ClusterView{
		"dev":  completedFrom(rolloutsv1alpha1.PhaseCompleted),
		"test": completedDestination(),
		"acc":  readyTo(false),
		"prod": readyTo(false),
	}, writer)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 1 {
		t.Fatalf("triggers = %#v, want one Start for acc", writer.triggers)
	}
	assertHop(t, hops, "test", "acc", testRevision)
	for _, hop := range hops {
		if hop.To == "test" && (hop.Action == ActionWriteStart || hop.Reason == ReasonAlreadyWritten) {
			t.Fatalf("test was written: %#v", hop)
		}
		if hop.To == "prod" && (hop.Action == ActionWriteStart || hop.Reason == ReasonAlreadyWritten) {
			t.Fatalf("prod was written: %#v", hop)
		}
	}
}

func completedPipelineFrom() (time.Time, ClusterView) {
	completedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	from := completedFrom(rolloutsv1alpha1.PhaseCompleted)
	from.State.CompletedAt = &metav1.Time{Time: completedAt}
	return completedAt, from
}

func TestReconcilePipelineApprovalGate(t *testing.T) {
	t.Parallel()
	_, from := completedPipelineFrom()
	approve := rolloutsv1alpha1.PromotionTrigger{
		Cluster: "test", Action: rolloutsv1alpha1.PromotionTriggerApprove, Revision: testRevision,
	}
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{
		{Name: "dev"},
		{Name: "test", Approval: rolloutsv1alpha1.PromotionApprovalApprove},
	}, nil)
	writer := &memWriter{}
	views := map[string]map[string]ClusterView{
		"dev":  {testRollout: from},
		"test": {testRollout: readyTo(false)},
	}
	hops, err := reconcilePrepared(t, pipeline, views, writer, time.Time{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 || hops[0].Reason != ReasonApprovalRequired {
		t.Fatalf("hop = %#v triggers %#v", hops[0], writer.triggers)
	}

	pipeline.Spec.Triggers = []rolloutsv1alpha1.PromotionTrigger{approve}
	hops, err = reconcilePrepared(t, pipeline, views, writer, time.Time{})
	if err != nil {
		t.Fatalf("approved reconcile: %v", err)
	}
	if len(writer.triggers) != 1 || writer.triggers[0].Revision != testRevision || hops[0].Reason != ReasonAlreadyWritten {
		t.Fatalf("triggers = %#v hop %#v", writer.triggers, hops[0])
	}
	if len(pipeline.Spec.Triggers) != 1 || pipeline.Spec.Triggers[0].Action != rolloutsv1alpha1.PromotionTriggerApprove {
		t.Fatalf("spec triggers = %#v", pipeline.Spec.Triggers)
	}
	if len(pipeline.Status.AcknowledgedTriggers) != 1 || pipeline.Status.AcknowledgedTriggers[0].Outcome != rollout.TriggerOutcomeApplied {
		t.Fatalf("acks = %#v", pipeline.Status.AcknowledgedTriggers)
	}
}

func TestReconcilePipelineInterClusterSoak(t *testing.T) {
	t.Parallel()
	completedAt, from := completedPipelineFrom()
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{
		{Name: "dev"},
		{Name: "test", Soak: &metav1.Duration{Duration: time.Hour}},
	}, nil)
	writer := &memWriter{}
	views := map[string]map[string]ClusterView{
		"dev":  {testRollout: from},
		"test": {testRollout: readyTo(false)},
	}
	hops, err := reconcilePrepared(t, pipeline, views, writer, completedAt.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 || hops[0].Reason != ReasonInterClusterSoak {
		t.Fatalf("hop = %#v triggers %#v", hops[0], writer.triggers)
	}
	hops, err = reconcilePrepared(t, pipeline, views, writer, completedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("elapsed reconcile: %v", err)
	}
	if len(writer.triggers) != 1 || hops[0].Reason != ReasonAlreadyWritten {
		t.Fatalf("triggers = %#v hop %#v", writer.triggers, hops[0])
	}
}

func TestReconcilePipelineRevisionMatch(t *testing.T) {
	t.Parallel()
	_, from := completedPipelineFrom()
	match := true
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{
		{Name: "dev"},
		{Name: "test", RevisionMatch: &match},
	}, nil)
	dest := readyTo(false)
	dest.LatestRevisionName = nextRevision
	writer := &memWriter{}
	hops, err := reconcilePrepared(t, pipeline, map[string]map[string]ClusterView{
		"dev":  {testRollout: from},
		"test": {testRollout: dest},
	}, writer, time.Time{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 || hops[0].Reason != ReasonRevisionMismatch || hops[0].Revision != testRevision {
		t.Fatalf("hop = %#v", hops[0])
	}
}

func TestReconcilePipelinePartialFleet(t *testing.T) {
	t.Parallel()
	_, from := completedPipelineFrom()
	percent := int32(100)
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{
		{Name: "dev"},
		{Name: "test", MinCompletedPercent: &percent},
	}, nil)
	other := "billing"
	done := from
	done.Spec.Compositions = []rolloutsv1alpha1.CompositionTarget{{Name: testComposition}}
	waiting := completedFrom(rolloutsv1alpha1.PhaseCanarying)
	waiting.Spec.Compositions = []rolloutsv1alpha1.CompositionTarget{{Name: testComposition}}
	writer := &memWriter{}
	hops, err := reconcilePreparedRollouts(t, pipeline, []string{testRollout, other}, map[string]map[string]ClusterView{
		"dev": {
			testRollout: done,
			other:       waiting,
		},
		"test": {
			testRollout: readyTo(false),
			other:       readyTo(false),
		},
	}, writer, time.Time{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("triggers = %#v", writer.triggers)
	}
	for _, hop := range hops {
		if hop.Rollout == testRollout && hop.To == "test" {
			if hop.Reason != ReasonFleetIncomplete || hop.Message != "1 of 2 matching CompositionRollouts have completed "+testRevision {
				t.Fatalf("hop = %#v", hop)
			}
		}
	}
}

func TestReconcilePipelineSkip(t *testing.T) {
	t.Parallel()
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{{Name: "dev"}, {Name: "test"}}, []rolloutsv1alpha1.PromotionTrigger{{
		Cluster: "dev", Action: rolloutsv1alpha1.PromotionTriggerSkip, Revision: testRevision,
	}})
	writer := &memWriter{}
	hops, err := reconcilePrepared(t, pipeline, map[string]map[string]ClusterView{
		"dev":  {testRollout: completedFrom(rolloutsv1alpha1.PhaseCanarying)},
		"test": {testRollout: readyTo(false)},
	}, writer, time.Time{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 1 || writer.triggers[0].Revision != testRevision {
		t.Fatalf("triggers = %#v hop %#v", writer.triggers, hops[0])
	}
	if len(pipeline.Spec.Triggers) != 1 {
		t.Fatalf("spec triggers = %#v", pipeline.Spec.Triggers)
	}
	if len(pipeline.Status.AcknowledgedTriggers) != 1 || pipeline.Status.AcknowledgedTriggers[0].Action != rolloutsv1alpha1.PromotionTriggerSkip {
		t.Fatalf("acks = %#v", pipeline.Status.AcknowledgedTriggers)
	}
}

func TestReconcilePipelineDryRunChain(t *testing.T) {
	t.Parallel()
	on := true
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{{Name: "dev"}, {Name: "test"}}, nil)
	pipeline.Spec.DryRunChain = &on
	writer := &memWriter{}
	hops, err := reconcilePrepared(t, pipeline, map[string]map[string]ClusterView{
		"dev":  {testRollout: finishedSimulation()},
		"test": {testRollout: readyTo(false)},
	}, writer, time.Time{})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writer.triggers) != 0 {
		t.Fatalf("triggers = %#v, want none", writer.triggers)
	}
	if hops[0].Reason != ReasonDryRunRecorded || hops[0].WouldStart == nil || hops[0].WouldStart.Revision != testRevision {
		t.Fatalf("hop = %#v", hops[0])
	}
	if hops[0].WouldStart.Action != rolloutsv1alpha1.TriggerStart {
		t.Fatalf("wouldStart = %#v", hops[0].WouldStart)
	}
}

func gatePipeline(clusters []rolloutsv1alpha1.PromotionCluster, triggers []rolloutsv1alpha1.PromotionTrigger) *rolloutsv1alpha1.PromotionPipeline {
	pipeline := &rolloutsv1alpha1.PromotionPipeline{}
	pipeline.Name = "environments"
	pipeline.Spec.Clusters = clusters
	pipeline.Spec.Triggers = triggers
	return pipeline
}

func reconcileFixture(t *testing.T, clusters []string, views map[string]ClusterView, writer *memWriter) ([]rolloutsv1alpha1.PromotionHopStatus, error) {
	t.Helper()
	spec := make([]rolloutsv1alpha1.PromotionCluster, 0, len(clusters))
	for _, name := range clusters {
		spec = append(spec, rolloutsv1alpha1.PromotionCluster{Name: name})
	}
	byRollout := make(map[string]map[string]ClusterView, len(views))
	for cluster, view := range views {
		byRollout[cluster] = map[string]ClusterView{testRollout: view}
	}
	return reconcilePrepared(t, gatePipeline(spec, nil), byRollout, writer, time.Time{})
}

func reconcilePrepared(t *testing.T, pipeline *rolloutsv1alpha1.PromotionPipeline, views map[string]map[string]ClusterView, writer *memWriter, now time.Time) ([]rolloutsv1alpha1.PromotionHopStatus, error) {
	t.Helper()
	return reconcilePreparedRollouts(t, pipeline, []string{testRollout}, views, writer, now)
}

func reconcilePreparedRollouts(t *testing.T, pipeline *rolloutsv1alpha1.PromotionPipeline, rollouts []string, views map[string]map[string]ClusterView, writer *memWriter, now time.Time) ([]rolloutsv1alpha1.PromotionHopStatus, error) {
	t.Helper()
	compositions := make(map[string][]string, len(rollouts))
	for _, name := range rollouts {
		compositions[name] = []string{testComposition}
	}
	ctx := withHopSource(context.Background(), hopSource{
		rollouts:     append([]string(nil), rollouts...),
		compositions: compositions,
	})
	if !now.IsZero() {
		ctx = withNow(ctx, now)
	}
	read := func(clusterName, rolloutName, composition, _ string) (ClusterView, error) {
		if composition != testComposition {
			return ClusterView{Engaged: true, Found: false}, nil
		}
		byRollout, ok := views[clusterName]
		if !ok {
			return ClusterView{Engaged: false}, nil
		}
		view, ok := byRollout[rolloutName]
		if !ok {
			return ClusterView{Engaged: true, Found: false}, nil
		}
		return view, nil
	}
	return reconcilePipeline(ctx, pipeline, read, writer, func(string) (client.Client, error) {
		return nil, nil
	})
}

func assertHop(t *testing.T, hops []rolloutsv1alpha1.PromotionHopStatus, from, to, revision string) {
	t.Helper()
	for _, hop := range hops {
		if hop.From == from && hop.To == to {
			if hop.Composition != testComposition || hop.Rollout != testRollout || hop.Revision != revision {
				t.Fatalf("hop %s -> %s = %#v", from, to, hop)
			}
			return
		}
	}
	t.Fatalf("missing hop %s -> %s in %#v", from, to, hops)
}
