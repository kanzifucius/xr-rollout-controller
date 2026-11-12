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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	view := ClusterView{Engaged: true, Found: true, Spec: cr.Spec}
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

func revisionNumber(obj *unstructured.Unstructured) (int64, bool) {
	number, found, err := unstructured.NestedInt64(obj.Object, "spec", "revision")
	if err != nil || !found {
		return 0, false
	}
	return number, true
}
