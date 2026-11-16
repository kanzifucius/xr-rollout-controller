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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/cli/tui"
)

type triggerWriter struct {
	client client.Client
}

func (w triggerWriter) AppendRollout(ctx context.Context, name string, trigger rolloutsv1alpha1.RolloutTrigger) error {
	return AppendRolloutTrigger(ctx, w.client, name, trigger)
}

func (w triggerWriter) AppendPipeline(ctx context.Context, name string, trigger rolloutsv1alpha1.PromotionTrigger) error {
	return AppendPipelineTrigger(ctx, w.client, name, trigger)
}

func loadSnapshot(ctx context.Context, cl client.Client) (tui.Snapshot, error) {
	var rollouts rolloutsv1alpha1.CompositionRolloutList
	if err := cl.List(ctx, &rollouts); err != nil {
		return tui.Snapshot{}, fmt.Errorf("list CompositionRollout: %w", err)
	}
	var pipelines rolloutsv1alpha1.PromotionPipelineList
	if err := cl.List(ctx, &pipelines); err != nil {
		return tui.Snapshot{}, fmt.Errorf("list PromotionPipeline: %w", err)
	}
	return tui.SnapshotFrom(rollouts.Items, pipelines.Items), nil
}
