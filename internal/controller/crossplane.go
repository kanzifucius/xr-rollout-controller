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

package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

var (
	compositionGVR = schema.GroupVersionResource{
		Group: compRevGVKGroup, Version: compRevGVKVersion, Resource: "compositions",
	}
	compositionRevisionGVR = schema.GroupVersionResource{
		Group: compRevGVKGroup, Version: compRevGVKVersion, Resource: "compositionrevisions",
	}
)

type revisionRecord struct {
	name     string
	revision int64
}

func (r *CompositionRolloutReconciler) latestCompositionRevision(ctx context.Context, compositionName string) (revisionRecord, error) {
	ul, err := r.Dynamic.Resource(compositionRevisionGVR).List(ctx, metav1.ListOptions{
		LabelSelector: labelCompositionName + "=" + compositionName,
	})
	if err != nil {
		return revisionRecord{}, err
	}

	var best revisionRecord
	for i := range ul.Items {
		rev := ul.Items[i]
		revNum, _, _ := unstructured.NestedInt64(rev.Object, "spec", "revision")
		name := rev.GetName()
		if best.name == "" || revNum > best.revision {
			best = revisionRecord{name: name, revision: revNum}
		}
	}
	if best.name == "" {
		return revisionRecord{}, fmt.Errorf("no composition revisions for %q", compositionName)
	}
	return best, nil
}

func (r *CompositionRolloutReconciler) compositeTypeGVK(ctx context.Context, compositionName string) (schema.GroupVersionKind, error) {
	comp, err := r.Dynamic.Resource(compositionGVR).Get(ctx, compositionName, metav1.GetOptions{})
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	apiVersion, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "apiVersion")
	kind, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "kind")
	if apiVersion == "" || kind == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("composition %q missing compositeTypeRef", compositionName)
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: kind}, nil
}

func (r *CompositionRolloutReconciler) listXRs(ctx context.Context, gvk schema.GroupVersionKind) ([]unstructured.Unstructured, error) {
	mapping, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	ul, err := r.Dynamic.Resource(mapping.Resource).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return ul.Items, nil
}

func fleetSourceRevision(managed []unstructured.Unstructured, revByName map[string]int64) rolloutsv1alpha1.RevisionInfo {
	counts := map[int64]int{}
	nameForNum := map[int64]string{}
	for i := range managed {
		ref := managed[i]
		revName, _, _ := unstructured.NestedString(ref.Object, "spec", "crossplane", "compositionRevisionRef", "name")
		if revName == "" {
			continue
		}
		revNum := revByName[revName]
		counts[revNum]++
		nameForNum[revNum] = revName
	}
	var bestNum int64 = -1
	for n, c := range counts {
		if bestNum < 0 || c > counts[bestNum] {
			bestNum = n
		}
	}
	if bestNum < 0 {
		return rolloutsv1alpha1.RevisionInfo{}
	}
	return rolloutsv1alpha1.RevisionInfo{Name: nameForNum[bestNum], Revision: bestNum}
}

func buildRevisionMap(ctx context.Context, r *CompositionRolloutReconciler, compositionName string) (map[string]int64, error) {
	ul, err := r.Dynamic.Resource(compositionRevisionGVR).List(ctx, metav1.ListOptions{
		LabelSelector: labelCompositionName + "=" + compositionName,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(ul.Items))
	for i := range ul.Items {
		rev := ul.Items[i]
		revNum, _, _ := unstructured.NestedInt64(rev.Object, "spec", "revision")
		out[rev.GetName()] = revNum
	}
	return out, nil
}
