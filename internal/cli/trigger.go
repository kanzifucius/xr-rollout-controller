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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

// FieldManager owns the CLI's trigger entries and nothing else.
const FieldManager = "xr-rollout-cli"

type rolloutKey struct {
	composition string
	action      string
	revision    string
}

type pipelineKey struct {
	cluster     string
	action      string
	revision    string
	rollout     string
	composition string
}

func rolloutTriggersForApply(live *rolloutsv1alpha1.CompositionRollout, next rolloutsv1alpha1.RolloutTrigger) []rolloutsv1alpha1.RolloutTrigger {
	owned := ownedRolloutKeys(live.GetManagedFields())
	nextKey := rolloutKey{next.Composition, string(next.Action), next.Revision}
	out := make([]rolloutsv1alpha1.RolloutTrigger, 0, len(owned)+1)
	for i := range live.Spec.Triggers {
		trigger := live.Spec.Triggers[i]
		key := rolloutKey{trigger.Composition, string(trigger.Action), trigger.Revision}
		if _, ok := owned[key]; !ok || key == nextKey {
			continue
		}
		out = append(out, trigger)
	}
	return append(out, next)
}

func pipelineTriggersForApply(live *rolloutsv1alpha1.PromotionPipeline, next rolloutsv1alpha1.PromotionTrigger) []rolloutsv1alpha1.PromotionTrigger {
	owned := ownedPipelineKeys(live.GetManagedFields())
	nextKey := pipelineKey{next.Cluster, string(next.Action), next.Revision, next.Rollout, next.Composition}
	out := make([]rolloutsv1alpha1.PromotionTrigger, 0, len(owned)+1)
	for i := range live.Spec.Triggers {
		trigger := live.Spec.Triggers[i]
		key := pipelineKey{trigger.Cluster, string(trigger.Action), trigger.Revision, trigger.Rollout, trigger.Composition}
		if _, ok := owned[key]; !ok || key == nextKey {
			continue
		}
		out = append(out, trigger)
	}
	return append(out, next)
}

func ownedRolloutKeys(entries []metav1.ManagedFieldsEntry) map[rolloutKey]struct{} {
	out := make(map[rolloutKey]struct{})
	for _, fields := range ownedTriggerFields(entries) {
		out[rolloutKey{fields["composition"], fields["action"], fields["revision"]}] = struct{}{}
	}
	return out
}

func ownedPipelineKeys(entries []metav1.ManagedFieldsEntry) map[pipelineKey]struct{} {
	out := make(map[pipelineKey]struct{})
	for _, fields := range ownedTriggerFields(entries) {
		out[pipelineKey{
			fields["cluster"],
			fields["action"],
			fields["revision"],
			fields["rollout"],
			fields["composition"],
		}] = struct{}{}
	}
	return out
}

// ownedTriggerFields returns the list-map keys under spec.triggers that
// FieldManager already owns. Other managers' keys are absent, so an apply
// of this set does not take them.
func ownedTriggerFields(entries []metav1.ManagedFieldsEntry) []map[string]string {
	var out []map[string]string
	for i := range entries {
		entry := entries[i]
		if entry.Manager != FieldManager || entry.FieldsV1 == nil || len(entry.FieldsV1.Raw) == 0 {
			continue
		}
		var root map[string]any
		if err := json.Unmarshal(entry.FieldsV1.Raw, &root); err != nil {
			continue
		}
		triggers, ok := nestedMap(root, "f:spec", "f:triggers")
		if !ok {
			continue
		}
		for key := range triggers {
			fields, ok := decodeListKey(key)
			if !ok {
				continue
			}
			out = append(out, fields)
		}
	}
	return out
}

func nestedMap(root map[string]any, path ...string) (map[string]any, bool) {
	current := root
	for _, step := range path {
		next, ok := current[step].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// AppendRolloutTrigger server-side applies one CompositionRollout trigger.
// The patch body is only spec.triggers. Entries this field manager already
// owns are sent again so they stay. Entries owned by anyone else are left out
// of the patch and stay on the object.
func AppendRolloutTrigger(ctx context.Context, cl client.Client, rolloutName string, trigger rolloutsv1alpha1.RolloutTrigger) error {
	live := &rolloutsv1alpha1.CompositionRollout{}
	if err := cl.Get(ctx, client.ObjectKey{Name: rolloutName}, live); err != nil {
		return fmt.Errorf("get CompositionRollout %s: %w", rolloutName, err)
	}
	if err := applyTriggers(ctx, cl, "CompositionRollout", rolloutName, rolloutTriggersForApply(live, trigger)); err != nil {
		return fmt.Errorf("apply %s on %s: %w", trigger.Action, rolloutName, err)
	}
	return nil
}

// AppendPipelineTrigger server-side applies one PromotionPipeline trigger.
func AppendPipelineTrigger(ctx context.Context, cl client.Client, pipelineName string, trigger rolloutsv1alpha1.PromotionTrigger) error {
	live := &rolloutsv1alpha1.PromotionPipeline{}
	if err := cl.Get(ctx, client.ObjectKey{Name: pipelineName}, live); err != nil {
		return fmt.Errorf("get PromotionPipeline %s: %w", pipelineName, err)
	}
	if err := applyTriggers(ctx, cl, "PromotionPipeline", pipelineName, pipelineTriggersForApply(live, trigger)); err != nil {
		return fmt.Errorf("apply %s on %s: %w", trigger.Action, pipelineName, err)
	}
	return nil
}

func applyTriggers(ctx context.Context, cl client.Client, kind, name string, triggers any) error {
	raw, err := json.Marshal(triggers)
	if err != nil {
		return fmt.Errorf("encode triggers: %w", err)
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("encode triggers: %w", err)
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind(kind))
	obj.SetName(name)
	if err := unstructured.SetNestedSlice(obj.Object, items, "spec", "triggers"); err != nil {
		return fmt.Errorf("set triggers: %w", err)
	}
	if err := cl.Patch(ctx, obj, client.Apply, client.FieldOwner(FieldManager)); err != nil {
		return err
	}
	return nil
}

func decodeListKey(key string) (map[string]string, bool) {
	const prefix = "k:"
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		return nil, false
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(key[len(prefix):]), &fields); err != nil {
		return nil, false
	}
	return fields, true
}
