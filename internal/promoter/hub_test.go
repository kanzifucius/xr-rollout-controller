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

const (
	fromCluster = "dev"
	toCluster   = "acc"
	fromObject  = "payments-dev"
	toObject    = "payments-acc"
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
	from.Name = fromObject
	to := readyTo(false)
	to.Name = toObject
	writer := &recordingWriter{}
	pipeline := gatePipeline([]rolloutsv1alpha1.PromotionCluster{{Name: fromCluster}, {Name: toCluster}}, nil)
	ctx := withHopSource(context.Background(), hopSource{
		rollouts:     []string{""},
		compositions: map[string][]string{"": {testComposition}},
	})
	read := func(clusterName, _, composition, _ string) (ClusterView, error) {
		if composition != testComposition {
			return ClusterView{Engaged: true}, nil
		}
		switch clusterName {
		case fromCluster:
			return from, nil
		case toCluster:
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
	if writer.rollout != toObject {
		t.Fatalf("start target = %s", writer.rollout)
	}
	if len(hops) != 1 || hops[0].Rollout != toObject {
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
	dev.Name = fromObject
	dev.Spec.Cluster = fromCluster
	acc := &rolloutsv1alpha1.CompositionRollout{}
	acc.Name = toObject
	acc.Spec.Cluster = toCluster
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dev, acc).Build()
	got, err := hubRolloutForCluster(context.Background(), cl, toCluster)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.Name != acc.Name {
		t.Fatalf("object = %#v", got)
	}
	extra := acc.DeepCopy()
	extra.Name = "payments-acc-b"
	cl = fake.NewClientBuilder().WithScheme(scheme).WithObjects(acc, extra).Build()
	if _, err := hubRolloutForCluster(context.Background(), cl, toCluster); err == nil {
		t.Fatal("expected two objects for one cluster to fail")
	}
}
