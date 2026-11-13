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
	"errors"
	"fmt"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func (r *PromoterReconciler) readHubView(ctx context.Context, clusterName, composition, wantRevision string) (ClusterView, error) {
	spoke, err := r.clientFor(ctx, clusterName)
	if err != nil {
		if errors.Is(err, multicluster.ErrClusterNotFound) {
			return ClusterView{Engaged: false}, nil
		}
		return ClusterView{}, err
	}
	cr, err := hubRolloutForCluster(ctx, r.Local, clusterName)
	if err != nil {
		return ClusterView{}, err
	}
	if cr == nil {
		return ClusterView{Engaged: true, Found: false}, nil
	}
	view := ClusterView{
		Engaged: true,
		Found:   true,
		Name:    cr.Name,
		Spec:    cr.Spec,
		Labels:  cr.GetLabels(),
	}
	for i := range cr.Status.Rollouts {
		if cr.Status.Rollouts[i].Composition == composition {
			view.State = cr.Status.Rollouts[i]
			view.HasState = true
			break
		}
	}
	latest, present, err := latestRevision(ctx, spoke, composition, wantRevision)
	if err != nil {
		return ClusterView{}, err
	}
	view.LatestRevisionName = latest
	view.RevisionPresent = present
	return view, nil
}

func (r *PromoterReconciler) hubCatalog(ctx context.Context, pipeline *rolloutsv1alpha1.PromotionPipeline) (hopSource, error) {
	if r.Local == nil {
		return hopSource{}, fmt.Errorf("hub client is not configured")
	}
	wanted := map[string]struct{}{}
	for _, cluster := range pipeline.Spec.Clusters {
		wanted[cluster.Name] = struct{}{}
	}
	list := &rolloutsv1alpha1.CompositionRolloutList{}
	if err := r.Local.List(ctx, list); err != nil {
		return hopSource{}, fmt.Errorf("list CompositionRollout: %w", err)
	}
	seen := map[string]struct{}{}
	comps := map[string]struct{}{}
	for i := range list.Items {
		item := &list.Items[i]
		name := item.Spec.Cluster
		if name == "" {
			continue
		}
		if _, ok := wanted[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			return hopSource{}, fmt.Errorf("cluster %s: more than one CompositionRollout", name)
		}
		seen[name] = struct{}{}
		for j := range item.Spec.Compositions {
			comps[item.Spec.Compositions[j].Name] = struct{}{}
		}
	}
	names := make([]string, 0, len(comps))
	for name := range comps {
		names = append(names, name)
	}
	sort.Strings(names)
	return hopSource{
		rollouts:     []string{""},
		compositions: map[string][]string{"": names},
	}, nil
}

func hubRolloutForCluster(ctx context.Context, cl client.Client, clusterName string) (*rolloutsv1alpha1.CompositionRollout, error) {
	if cl == nil {
		return nil, fmt.Errorf("hub client is not configured")
	}
	list := &rolloutsv1alpha1.CompositionRolloutList{}
	if err := cl.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list CompositionRollout: %w", err)
	}
	var found *rolloutsv1alpha1.CompositionRollout
	for i := range list.Items {
		item := &list.Items[i]
		if item.Spec.Cluster != clusterName {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("cluster %s: more than one CompositionRollout", clusterName)
		}
		found = item.DeepCopy()
	}
	return found, nil
}
