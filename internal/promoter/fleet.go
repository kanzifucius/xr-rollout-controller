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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

var compositionRevisionListGVK = schema.GroupVersionKind{
	Group: "apiextensions.crossplane.io", Version: "v1", Kind: "CompositionRevisionList",
}

func viewComposition(ctx context.Context, cl client.Client, rolloutName, composition, wantRevision string) (ClusterView, error) {
	cr := &rolloutsv1alpha1.CompositionRollout{}
	if err := cl.Get(ctx, client.ObjectKey{Name: rolloutName}, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ClusterView{Engaged: true, Found: false}, nil
		}
		return ClusterView{}, fmt.Errorf("get CompositionRollout %s: %w", rolloutName, err)
	}
	view := ClusterView{Engaged: true, Found: true, Spec: cr.Spec, Labels: cr.GetLabels()}
	for i := range cr.Status.Rollouts {
		if cr.Status.Rollouts[i].Composition == composition {
			view.State = cr.Status.Rollouts[i]
			view.HasState = true
			break
		}
	}
	latest, present, err := latestRevision(ctx, cl, composition, wantRevision)
	if err != nil {
		return ClusterView{}, err
	}
	view.LatestRevisionName = latest
	view.RevisionPresent = present
	return view, nil
}

func latestRevision(ctx context.Context, cl client.Client, composition, wantRevision string) (string, bool, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(compositionRevisionListGVK)
	if err := cl.List(ctx, list, client.MatchingLabels{xp.CompositionNameLabel: composition}); err != nil {
		return "", false, fmt.Errorf("list CompositionRevision for %s: %w", composition, err)
	}
	var (
		latest  string
		best    int64
		have    bool
		present bool
	)
	for i := range list.Items {
		item := &list.Items[i]
		if wantRevision != "" && item.GetName() == wantRevision {
			present = true
		}
		number, ok := revisionNumber(item)
		if !ok {
			continue
		}
		if !have || number > best {
			have = true
			best = number
			latest = item.GetName()
		}
	}
	return latest, present, nil
}

func countFleet(
	ctx context.Context,
	pipeline *rolloutsv1alpha1.PromotionPipeline,
	fromName, composition, revision string,
	selector *metav1.LabelSelector,
	read func(clusterName, rolloutName, composition, wantRevision string) (ClusterView, error),
	clFor func(clusterName string) (client.Client, error),
) (int, int, error) {
	names, compositions, err := catalogFor(ctx, pipeline, clFor)
	if err != nil {
		return 0, 0, err
	}
	matching := 0
	completed := 0
	for _, rolloutName := range names {
		if !containsName(compositions[rolloutName], composition) {
			continue
		}
		view, err := read(fromName, rolloutName, composition, revision)
		if err != nil {
			return 0, 0, err
		}
		inFleet, err := inPromotionFleet(view, composition, selector)
		if err != nil {
			return 0, 0, err
		}
		if !inFleet {
			continue
		}
		matching++
		if fleetCompleted(pipeline, view, fromName, rolloutName, composition, revision) {
			completed++
		}
	}
	return matching, completed, nil
}

func inPromotionFleet(view ClusterView, composition string, selector *metav1.LabelSelector) (bool, error) {
	if !view.Found || !governs(view.Spec, composition) {
		return false, nil
	}
	return fleetSelectorMatches(selector, view.Labels)
}

func fleetCompleted(pipeline *rolloutsv1alpha1.PromotionPipeline, view ClusterView, fromName, rolloutName, composition, revision string) bool {
	if viewIsDryRun(view) {
		return false
	}
	if rev, ok := matchingSkip(pipeline, fromName, rolloutName, composition); ok && rev == revision {
		return true
	}
	got, ok := completedRevision(view)
	return ok && got == revision
}

func fleetSelectorMatches(selector *metav1.LabelSelector, lbls map[string]string) (bool, error) {
	if selector == nil {
		return true, nil
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("parse fleet selector: %w", err)
	}
	return parsed.Matches(labels.Set(lbls)), nil
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func revisionNumber(obj *unstructured.Unstructured) (int64, bool) {
	number, found, err := unstructured.NestedInt64(obj.Object, "spec", "revision")
	if err != nil || !found {
		return 0, false
	}
	return number, true
}
