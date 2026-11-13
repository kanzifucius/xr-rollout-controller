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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mccontroller "sigs.k8s.io/multicluster-runtime/pkg/controller"
	mchandler "sigs.k8s.io/multicluster-runtime/pkg/handler"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"
	mcsource "sigs.k8s.io/multicluster-runtime/pkg/source"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

// ClusterSource is the engaged-cluster view the remote reconciler needs.
// The kubeconfig provider implements it. This package does not import the promoter.
type ClusterSource interface {
	Get(ctx context.Context, clusterName string) (cluster.Cluster, error)
	Environment(clusterName string) (string, bool)
}

// RemoteReconciler reconciles CompositionRollout on each engaged cluster.
type RemoteReconciler struct {
	Inner    *CompositionRolloutReconciler
	Clusters ClusterSource
	mc       mccontroller.Controller
}

// Reconcile runs the rollout state machine against the cluster that owns the object.
// An empty cluster name is the local manager, which this controller does not serve.
func (r *RemoteReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	if req.ClusterName == "" {
		return ctrl.Result{}, nil
	}
	cc, err := r.clientsFor(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}
	return r.Inner.reconcile(ctx, cc, req.Request)
}

func (r *RemoteReconciler) clientsFor(ctx context.Context, clusterName string) (clusterClients, error) {
	env, ok := r.Clusters.Environment(clusterName)
	if !ok || env == "" {
		return clusterClients{}, fmt.Errorf("cluster %s: kubeconfig Secret has no environment annotation", clusterName)
	}
	cl, err := r.Clusters.Get(ctx, clusterName)
	if err != nil {
		return clusterClients{}, fmt.Errorf("get cluster %s: %w", clusterName, err)
	}
	cfg := cl.GetConfig()
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return clusterClients{}, fmt.Errorf("dynamic client for %s: %w", clusterName, err)
	}
	return clusterClients{
		client:      cl.GetClient(),
		dynamic:     dyn,
		mapper:      cl.GetRESTMapper(),
		recorder:    cl.GetEventRecorderFor("xr-rollout-controller"),
		environment: env,
		cluster:     clusterName,
		cache:       cl.GetCache(),
		scheme:      cl.GetScheme(),
		clusterObj:  cl,
	}, nil
}

// SetupWithManager registers CompositionRollout, CompositionRevision, and Composition
// watches on provider clusters only. Call it before the manager starts.
func (r *RemoteReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	if r.Inner == nil {
		return fmt.Errorf("remote reconciler has no inner CompositionRolloutReconciler")
	}
	r.Inner.remote = r
	if err := indexEngagedRollouts(mgr); err != nil {
		return err
	}
	rev := &unstructured.Unstructured{}
	rev.SetGroupVersionKind(compositionRevisionGVK)
	comp := &unstructured.Unstructured{}
	comp.SetGroupVersionKind(compositionGVK)
	noLocal := mcbuilder.WithEngageWithLocalCluster(false)
	bld := mcbuilder.ControllerManagedBy(mgr).
		Named("compositionrollout-remote").
		For(&rolloutsv1alpha1.CompositionRollout{}, noLocal).
		Watches(rev, r.enqueueRevision, noLocal, mcbuilder.WithPredicates(createOnlyPredicate())).
		Watches(comp, r.enqueueComposition, noLocal)
	mc, err := bld.Build(r)
	if err != nil {
		return err
	}
	r.mc = mc
	return nil
}

func indexEngagedRollouts(mgr mcmanager.Manager) error {
	return mgr.GetFieldIndexer().IndexField(context.Background(), &rolloutsv1alpha1.CompositionRollout{}, FieldIndexCompositionName, compositionNameIndex)
}

func (r *RemoteReconciler) enqueueRevision(clusterName string, cl cluster.Cluster) handler.TypedEventHandler[client.Object, mcreconcile.Request] {
	inner := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
		compName := u.GetLabels()[xp.CompositionNameLabel]
		if compName == "" {
			return nil
		}
		return r.requests(ctx, cl.GetClient(), compName)
	})
	return mchandler.ForCluster(inner, clusterName)
}

func (r *RemoteReconciler) enqueueComposition(clusterName string, cl cluster.Cluster) handler.TypedEventHandler[client.Object, mcreconcile.Request] {
	inner := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return r.requests(ctx, cl.GetClient(), obj.GetName())
	})
	return mchandler.ForCluster(inner, clusterName)
}

func (r *RemoteReconciler) requests(ctx context.Context, cl client.Client, compName string) []reconcile.Request {
	var list rolloutsv1alpha1.CompositionRolloutList
	if err := cl.List(ctx, &list, client.MatchingFields{FieldIndexCompositionName: compName}); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: list.Items[i].Name}})
	}
	return reqs
}

// watchRemoteXRs registers a lazy XR watch on one engaged cluster.
// v0.20.4-alpha.7 drops engaged clusters from the controller map as soon as
// Engage returns, so MultiClusterWatch cannot see them. ForCluster plus Watch
// attaches the informer to the cluster that is reconciling now.
func (r *CompositionRolloutReconciler) watchRemoteXRs(cc clusterClients, key xrWatchKey, gvk schema.GroupVersionKind) error {
	if cc.clusterObj == nil || r.remote == nil || r.remote.mc == nil {
		return fmt.Errorf("cluster %s: XR watch has no cluster", cc.cluster)
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	if cc.scheme != nil {
		cc.scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		cc.scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	}
	handlerFn := func(clusterName string, cl cluster.Cluster) handler.TypedEventHandler[*unstructured.Unstructured, mcreconcile.Request] {
		inner := handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj *unstructured.Unstructured) []reconcile.Request {
			compName := xp.GetCompositionRefName(obj)
			if compName == "" {
				return nil
			}
			return r.remote.requests(ctx, cl.GetClient(), compName)
		})
		return mchandler.TypedForCluster(inner, clusterName)
	}
	src, err := mcsource.TypedKind(u, handlerFn).ForCluster(cc.cluster, cc.clusterObj)
	if err != nil {
		return fmt.Errorf("xr watch for %s: %w", cc.cluster, err)
	}
	if err := r.remote.mc.Watch(src); err != nil {
		return fmt.Errorf("watch %s on %s: %w", gvk, cc.cluster, err)
	}
	r.xrWatches.Store(key, cc.clusterObj)
	return nil
}
