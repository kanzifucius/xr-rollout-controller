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

package tui

import (
	"sort"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

// SnapshotFrom builds the terminal UI rows from the current objects.
func SnapshotFrom(rollouts []rolloutsv1alpha1.CompositionRollout, pipelines []rolloutsv1alpha1.PromotionPipeline) Snapshot {
	snap := Snapshot{
		Rows:      make([]Row, 0, len(rollouts)),
		Pipelines: make([]Pipeline, 0, len(pipelines)),
	}
	for i := range rollouts {
		item := &rollouts[i]
		if len(item.Status.Rollouts) == 0 {
			snap.Rows = append(snap.Rows, Row{
				Rollout:     item.Name,
				Composition: onlyComposition(item),
				Cluster:     item.Spec.Cluster,
			})
			continue
		}
		for j := range item.Status.Rollouts {
			state := &item.Status.Rollouts[j]
			row := Row{
				Rollout:     item.Name,
				Composition: state.Composition,
				Cluster:     item.Spec.Cluster,
				Phase:       string(state.Phase),
				Revision:    state.TargetRevision.Name,
				Canary:      canaryNames(&state.Canary),
			}
			if state.Canary.Healthy != 0 || state.Canary.Unhealthy != 0 || len(state.Canary.Selected) != 0 {
				row.CanaryHealthy = state.Canary.Healthy
				row.CanaryUnhealthy = state.Canary.Unhealthy
			}
			if state.PauseReason != nil {
				row.PauseReason = state.PauseReason.Reason
				row.PauseMessage = state.PauseReason.Message
			}
			snap.Rows = append(snap.Rows, row)
		}
	}
	sort.Slice(snap.Rows, func(i, j int) bool {
		if snap.Rows[i].Rollout != snap.Rows[j].Rollout {
			return snap.Rows[i].Rollout < snap.Rows[j].Rollout
		}
		return snap.Rows[i].Composition < snap.Rows[j].Composition
	})
	for i := range pipelines {
		item := &pipelines[i]
		clusters := make([]string, 0, len(item.Spec.Clusters))
		for j := range item.Spec.Clusters {
			clusters = append(clusters, item.Spec.Clusters[j].Name)
		}
		snap.Pipelines = append(snap.Pipelines, Pipeline{Name: item.Name, Clusters: clusters})
	}
	sort.Slice(snap.Pipelines, func(i, j int) bool { return snap.Pipelines[i].Name < snap.Pipelines[j].Name })
	return snap
}

func canaryNames(canary *rolloutsv1alpha1.CanaryStatus) []string {
	if canary == nil || len(canary.Selected) == 0 {
		return nil
	}
	names := make([]string, 0, len(canary.Selected))
	for i := range canary.Selected {
		names = append(names, canary.Selected[i].String())
	}
	return names
}

func onlyComposition(obj *rolloutsv1alpha1.CompositionRollout) string {
	if len(obj.Spec.Compositions) == 1 {
		return obj.Spec.Compositions[0].Name
	}
	return ""
}
