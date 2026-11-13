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
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

const (
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
	rev := &unstructured.Unstructured{}
	rev.SetGroupVersionKind(compositionRevisionGVK)
	comp := &unstructured.Unstructured{}
	comp.SetGroupVersionKind(compositionGVK)

	return bld.
		Watches(rev, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return r.mapCompositionRevision(ctx, localClients(r), obj)
		}), builder.WithPredicates(createOnlyPredicate())).
		Watches(comp, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return r.mapComposition(ctx, localClients(r), obj)
		}))
}

func createOnlyPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		UpdateFunc:  func(event.UpdateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func (r *CompositionRolloutReconciler) mapCompositionRevision(ctx context.Context, cc clusterClients, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	compName := u.GetLabels()[xp.CompositionNameLabel]
	if compName == "" {
		return nil
	}
	return r.requestsForComposition(ctx, cc, compName)
}

func (r *CompositionRolloutReconciler) mapComposition(ctx context.Context, cc clusterClients, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	return r.requestsForComposition(ctx, cc, u.GetName())
}

func (r *CompositionRolloutReconciler) mapXRToRollout(ctx context.Context, cc clusterClients, obj client.Object) []reconcile.Request {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	compName := xp.GetCompositionRefName(u)
	if compName == "" {
		return nil
	}
	return r.requestsForComposition(ctx, cc, compName)
}

func (r *CompositionRolloutReconciler) requestsForComposition(ctx context.Context, cc clusterClients, compName string) []reconcile.Request {
	crList, err := r.listRolloutsForComposition(ctx, cc, compName)
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

type xrWatchKey struct {
	cluster string
	gvk     schema.GroupVersionKind
}

func (r *CompositionRolloutReconciler) ensureXRWatch(cc clusterClients, gvk schema.GroupVersionKind) error {
	key := xrWatchKey{cluster: cc.cluster, gvk: gvk}
	if xrWatchCurrent(&r.xrWatches, key, cc) {
		return nil
	}
	r.xrWatchMu.Lock()
	defer r.xrWatchMu.Unlock()
	if xrWatchCurrent(&r.xrWatches, key, cc) {
		return nil
	}
	if cc.cluster != "" {
		return r.watchRemoteXRs(cc, key, gvk)
	}
	if cc.controller == nil {
		return nil
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	if cc.scheme != nil {
		cc.scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		cc.scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	}

	typedHandler := handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj *unstructured.Unstructured) []reconcile.Request {
		return r.mapXRToRollout(ctx, cc, obj)
	})
	if err := cc.controller.Watch(source.Kind(cc.cache, u, typedHandler)); err != nil {
		return err
	}
	r.xrWatches.Store(key, struct{}{})
	return nil
}

func xrWatchCurrent(watches *sync.Map, key xrWatchKey, cc clusterClients) bool {
	prev, loaded := watches.Load(key)
	if !loaded {
		return false
	}
	if cc.cluster == "" {
		return true
	}
	prevCluster, ok := prev.(cluster.Cluster)
	return ok && prevCluster == cc.clusterObj
}

var _ controller.Controller
