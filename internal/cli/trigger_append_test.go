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

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func TestAppendTriggerDoesNotDropExistingEntries(t *testing.T) {
	cl := startEnvtest(t)
	ctx := context.Background()
	const composition = "platformdb.example.org"

	rollout := baseRollout("team-payments")
	rollout.Spec.Triggers = []rolloutsv1alpha1.RolloutTrigger{{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerStart,
		Revision:    "rev-old",
		Actor:       "xr-rollout-promoter",
	}}
	if err := cl.Patch(ctx, rollout, client.Apply, client.FieldOwner("xr-rollout-promoter")); err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	approve := rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerApprove,
		Revision:    "rev-a",
		Actor:       "first",
	}
	if err := AppendRolloutTrigger(ctx, cl, "team-payments", approve); err != nil {
		t.Fatalf("append Approve: %v", err)
	}
	abort := rolloutsv1alpha1.RolloutTrigger{
		Composition: composition,
		Action:      rolloutsv1alpha1.TriggerAbort,
		Revision:    "rev-b",
		Actor:       "cli",
	}
	if err := AppendRolloutTrigger(ctx, cl, "team-payments", abort); err != nil {
		t.Fatalf("append Abort: %v", err)
	}

	got := &rolloutsv1alpha1.CompositionRollout{}
	if err := cl.Get(ctx, client.ObjectKey{Name: "team-payments"}, got); err != nil {
		t.Fatalf("get rollout: %v", err)
	}
	want := map[string]string{
		triggerID(composition, string(rolloutsv1alpha1.TriggerStart), "rev-old"): "xr-rollout-promoter",
		triggerID(composition, string(rolloutsv1alpha1.TriggerApprove), "rev-a"): "first",
		triggerID(composition, string(rolloutsv1alpha1.TriggerAbort), "rev-b"):   "cli",
	}
	if len(got.Spec.Triggers) != len(want) {
		t.Fatalf("triggers = %#v, want %d entries including the promoter Start", got.Spec.Triggers, len(want))
	}
	for i := range got.Spec.Triggers {
		trigger := got.Spec.Triggers[i]
		id := triggerID(trigger.Composition, string(trigger.Action), trigger.Revision)
		actor, ok := want[id]
		if !ok || actor != trigger.Actor {
			raw, _ := json.Marshal(got.GetManagedFields())
			t.Fatalf("trigger %s actor %q, want %q\nmanagedFields %s", id, trigger.Actor, actor, raw)
		}
	}
	if len(got.Spec.Compositions) != 1 || got.Spec.Compositions[0].Name != composition {
		t.Fatalf("compositions = %#v, want them left alone", got.Spec.Compositions)
	}
	if !hasManager(got.GetManagedFields(), "xr-rollout-promoter") || !hasManager(got.GetManagedFields(), FieldManager) {
		raw, _ := json.Marshal(got.GetManagedFields())
		t.Fatalf("managedFields = %s, want promoter and %s", raw, FieldManager)
	}

	pipeline := basePipeline("environments")
	pipeline.Spec.Triggers = []rolloutsv1alpha1.PromotionTrigger{{
		Cluster:  "acc",
		Action:   rolloutsv1alpha1.PromotionTriggerApprove,
		Revision: "rev-1",
		Actor:    "release",
	}}
	if err := cl.Patch(ctx, pipeline, client.Apply, client.FieldOwner("release-manager")); err != nil {
		t.Fatalf("seed pipeline: %v", err)
	}
	if err := AppendPipelineTrigger(ctx, cl, "environments", rolloutsv1alpha1.PromotionTrigger{
		Cluster:  "test",
		Action:   rolloutsv1alpha1.PromotionTriggerSkip,
		Revision: "rev-1",
		Rollout:  "team-payments",
		Actor:    "cli",
	}); err != nil {
		t.Fatalf("append Skip rev-1: %v", err)
	}
	if err := AppendPipelineTrigger(ctx, cl, "environments", rolloutsv1alpha1.PromotionTrigger{
		Cluster:  "test",
		Action:   rolloutsv1alpha1.PromotionTriggerSkip,
		Revision: "rev-2",
		Actor:    "cli",
	}); err != nil {
		t.Fatalf("append Skip rev-2: %v", err)
	}
	gotPipe := &rolloutsv1alpha1.PromotionPipeline{}
	if err := cl.Get(ctx, client.ObjectKey{Name: "environments"}, gotPipe); err != nil {
		t.Fatalf("get pipeline: %v", err)
	}
	pipeWant := map[string]string{
		pipelineID("acc", string(rolloutsv1alpha1.PromotionTriggerApprove), "rev-1", "", ""):            "release",
		pipelineID("test", string(rolloutsv1alpha1.PromotionTriggerSkip), "rev-1", "team-payments", ""): "cli",
		pipelineID("test", string(rolloutsv1alpha1.PromotionTriggerSkip), "rev-2", "", ""):              "cli",
	}
	if len(gotPipe.Spec.Triggers) != len(pipeWant) {
		raw, _ := json.Marshal(gotPipe.GetManagedFields())
		t.Fatalf("pipeline triggers = %#v, want %d\nmanagedFields %s", gotPipe.Spec.Triggers, len(pipeWant), raw)
	}
	for i := range gotPipe.Spec.Triggers {
		trigger := gotPipe.Spec.Triggers[i]
		id := pipelineID(trigger.Cluster, string(trigger.Action), trigger.Revision, trigger.Rollout, trigger.Composition)
		actor, ok := pipeWant[id]
		if !ok || actor != trigger.Actor {
			t.Fatalf("pipeline trigger %s actor %q, want %q in %#v", id, trigger.Actor, actor, gotPipe.Spec.Triggers)
		}
	}
}

func triggerID(composition, action, revision string) string {
	return composition + "/" + action + "/" + revision
}

func pipelineID(cluster, action, revision, rolloutName, composition string) string {
	return cluster + "/" + action + "/" + revision + "/" + rolloutName + "/" + composition
}

func hasManager(entries []metav1.ManagedFieldsEntry, manager string) bool {
	for i := range entries {
		if entries[i].Manager == manager {
			return true
		}
	}
	return false
}

func baseRollout(name string) *rolloutsv1alpha1.CompositionRollout {
	percent := int32(5)
	obj := &rolloutsv1alpha1.CompositionRollout{}
	obj.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("CompositionRollout"))
	obj.Name = name
	obj.Spec.Cluster = "prod"
	obj.Spec.Compositions = []rolloutsv1alpha1.CompositionTarget{{Name: "platformdb.example.org"}}
	obj.Spec.Canary.Strategy = rolloutsv1alpha1.CanaryStrategyPercent
	obj.Spec.Canary.Percent = &percent
	obj.Spec.Soak.Duration.Duration = time.Hour
	obj.Spec.Promotion.DefaultPolicy = rolloutsv1alpha1.PromotionManualApproval
	obj.Spec.Waves.BatchSize = intstr.FromString("10%")
	return obj
}

func basePipeline(name string) *rolloutsv1alpha1.PromotionPipeline {
	obj := &rolloutsv1alpha1.PromotionPipeline{}
	obj.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("PromotionPipeline"))
	obj.Name = name
	obj.Spec.Clusters = []rolloutsv1alpha1.PromotionCluster{{Name: "dev"}, {Name: "test"}, {Name: "acc"}}
	return obj
}

func startEnvtest(t *testing.T) client.Client {
	t.Helper()
	assets := envtestAssets()
	if assets == "" && os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("envtest binaries not found; run make setup-envtest or set KUBEBUILDER_ASSETS")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if assets != "" {
		env.BinaryAssetsDirectory = assets
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := env.Stop(); stopErr != nil {
			t.Errorf("envtest stop: %v", stopErr)
		}
	})
	scheme := runtime.NewScheme()
	if err := rolloutsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return cl
}

func envtestAssets() string {
	base := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(base, entry.Name())
		}
	}
	return ""
}
