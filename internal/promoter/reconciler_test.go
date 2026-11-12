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

	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
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

func reconcileFixture(t *testing.T, clusters []string, views map[string]ClusterView, writer *memWriter) ([]rolloutsv1alpha1.PromotionHopStatus, error) {
	t.Helper()
	pipeline := &rolloutsv1alpha1.PromotionPipeline{}
	pipeline.Name = "environments"
	for _, name := range clusters {
		pipeline.Spec.Clusters = append(pipeline.Spec.Clusters, rolloutsv1alpha1.PromotionCluster{Name: name})
	}
	ctx := withHopSource(context.Background(), hopSource{
		rollouts:     []string{testRollout},
		compositions: map[string][]string{testRollout: {testComposition}},
	})
	read := func(clusterName, rolloutName, composition, _ string) (ClusterView, error) {
		if rolloutName != testRollout || composition != testComposition {
			return ClusterView{Engaged: true, Found: false}, nil
		}
		view, ok := views[clusterName]
		if !ok {
			return ClusterView{Engaged: false}, nil
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
