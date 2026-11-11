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

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

const (
	labelCompositionName = "crossplane.io/composition-name"

	compRevGVKGroup   = "apiextensions.crossplane.io"
	compRevGVKVersion = "v1"
	compRevGVKKind    = "CompositionRevision"

	compGVKKind = "Composition"
)

var (
	compositionRevisionGVK = schema.GroupVersionKind{
		Group: compRevGVKGroup, Version: compRevGVKVersion, Kind: compRevGVKKind,
	}
	compositionGVK = schema.GroupVersionKind{
		Group: compRevGVKGroup, Version: compRevGVKVersion, Kind: compGVKKind,
	}
)

func (r *CompositionRolloutReconciler) setupStaticWatches(bld *builder.Builder) *builder.Builder {
	if r.apiInstalled(compositionRevisionGVK) {
		rev := &unstructured.Unstructured{}
		rev.SetGroupVersionKind(compositionRevisionGVK)
		bld = bld.Watches(rev, handler.EnqueueRequestsFromMapFunc(r.mapCompositionRevision), builder.WithPredicates(createOnlyPredicate()))
	}
	if r.apiInstalled(compositionGVK) {
		comp := &unstructured.Unstructured{}
		comp.SetGroupVersionKind(compositionGVK)
		bld = bld.Watches(comp, handler.EnqueueRequestsFromMapFunc(r.mapComposition))
	}
	return bld
}

// apiInstalled reports whether the apiserver serves gvk. A missing API is
// skipped so the manager can start before Crossplane is installed; a
// discovery error other than "no match" still attempts the watch.
func (r *CompositionRolloutReconciler) apiInstalled(gvk schema.GroupVersionKind) bool {
	if r.Mapper == nil {
		return true
	}
	_, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err == nil {
		return true
	}
	if meta.IsNoMatchError(err) {
		ctrl.Log.Info("skipping watch; API is not installed", "gvk", gvk.String())
		return false
	}
	ctrl.Log.Error(err, "discovery failed while checking watch; attempting it anyway", "gvk", gvk.String())
	return true
}

func createOnlyPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		UpdateFunc:  func(event.UpdateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func (r *CompositionRolloutReconciler) mapCompositionRevision(ctx context.Context, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	compName := u.GetLabels()[labelCompositionName]
	if compName == "" {
		return nil
	}
	return r.requestsForComposition(ctx, compName)
}

func (r *CompositionRolloutReconciler) mapComposition(ctx context.Context, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	return r.requestsForComposition(ctx, u.GetName())
}

func (r *CompositionRolloutReconciler) mapXRToRollout(ctx context.Context, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	compName := xp.GetCompositionRefName(u)
	if compName == "" {
		return nil
	}
	return r.requestsForComposition(ctx, compName)
}

func (r *CompositionRolloutReconciler) requestsForComposition(ctx context.Context, compName string) []reconcile.Request {
	crList, err := r.listRolloutsForComposition(ctx, compName)
	if err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(crList))
	for i := range crList {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: crList[i].Name},
		})
	}
	return reqs
}

func (r *CompositionRolloutReconciler) ensureXRWatch(gvk schema.GroupVersionKind) error {
	if _, loaded := r.xrWatches.Load(gvk); loaded {
		return nil
	}
	r.xrWatchMu.Lock()
	defer r.xrWatchMu.Unlock()
	if _, loaded := r.xrWatches.Load(gvk); loaded {
		return nil
	}
	if r.controller == nil {
		return nil
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	if r.Scheme != nil {
		r.Scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		r.Scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	}

	typedHandler := handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj *unstructured.Unstructured) []reconcile.Request {
		return r.mapXRToRollout(ctx, obj)
	})
	if err := r.controller.Watch(source.Kind(r.mgr.GetCache(), u, typedHandler)); err != nil {
		return err
	}
	r.xrWatches.Store(gvk, struct{}{})
	return nil
}

var _ controller.Controller
