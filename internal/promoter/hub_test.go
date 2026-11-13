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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

type recordingWriter struct {
	rollout string
}

func (w *recordingWriter) AppendStart(_ context.Context, _ client.Client, rolloutName string, _ rolloutsv1alpha1.RolloutTrigger) error {
	w.rollout = rolloutName
	return nil
}

func TestHubStartTargetsTheDestinationObject(t *testing.T) {
	t.Parallel()
	from := completedFrom(rolloutsv1alpha1.PhaseCompleted)
	from.Name = "payments-dev"
	to := readyTo(false)
	to.Name = "payments-acc"
	writer := &recordingWriter{}
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{{Name: "dev"}, {Name: "acc"}}, nil)
	ctx := withHopSource(context.Background(), hopSource{
		rollouts:     []string{""},
		compositions: map[string][]string{"": {testComposition}},
	})
	read := func(clusterName, _, composition, _ string) (ClusterView, error) {
		if composition != testComposition {
			return ClusterView{Engaged: true}, nil
		}
		switch clusterName {
		case "dev":
			return from, nil
		case "acc":
			return to, nil
		default:
			return ClusterView{Engaged: true}, nil
		}
	}
	hops, err := reconcilePipeline(ctx, pipeline, read, writer, func(string) (client.Client, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if writer.rollout != "payments-acc" {
		t.Fatalf("start target = %s", writer.rollout)
	}
	if len(hops) != 1 || hops[0].Rollout != "payments-acc" {
		t.Fatalf("hops = %#v", hops)
	}
}

func TestHubRolloutForCluster(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := rolloutsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	dev := &rolloutsv1alpha1.CompositionRollout{}
	dev.Name = "payments-dev"
	dev.Spec.Cluster = "dev"
	acc := &rolloutsv1alpha1.CompositionRollout{}
	acc.Name = "payments-acc"
	acc.Spec.Cluster = "acc"
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dev, acc).Build()
	got, err := hubRolloutForCluster(context.Background(), cl, "acc")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.Name != "payments-acc" {
		t.Fatalf("object = %#v", got)
	}
	extra := acc.DeepCopy()
	extra.Name = "payments-acc-b"
	cl = fake.NewClientBuilder().WithScheme(scheme).WithObjects(acc, extra).Build()
	if _, err := hubRolloutForCluster(context.Background(), cl, "acc"); err == nil {
		t.Fatal("expected two objects for one cluster to fail")
	}
}
