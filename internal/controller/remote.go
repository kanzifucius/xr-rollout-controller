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
	"errors"
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
	Engagement(clusterName string) (environment string, engaged bool)
}

// RemoteReconciler watches spoke Compositions, CompositionRevisions, and composite
// resources, and reconciles the hub CompositionRollout for that cluster.
// The CompositionRollout object and its status stay on the hub.
type RemoteReconciler struct {
	Inner     *CompositionRolloutReconciler
	Clusters  ClusterSource
	Hub       client.Client
	Namespace string
	mc        mccontroller.Controller
}

// Reconcile handles a spoke event that names a hub CompositionRollout.
// An empty cluster name is the local manager. The hub controller serves that queue.
func (r *RemoteReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	if req.ClusterName == "" || req.Name == "" || r.Inner == nil {
		return ctrl.Result{}, nil
	}
	return r.Inner.reconcileHub(ctx, req.Request)
}

// reconcileHub loads the hub object and patches composite resources on spec.cluster.
// An empty spec.cluster is an error: no patch and no status write.
func (r *CompositionRolloutReconciler) reconcileHub(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.remote == nil {
		return ctrl.Result{}, fmt.Errorf("remote reconciler is not configured")
	}
	cr := &rolloutsv1alpha1.CompositionRollout{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if cr.Spec.Cluster == "" {
		return ctrl.Result{}, fmt.Errorf("CompositionRollout %s: spec.cluster is required", cr.Name)
	}
	cc, err := r.remote.clientsFor(ctx, cr)
	if err != nil {
		var cred *credentialUnavailableError
		if errors.As(err, &cred) {
			return r.writeCredentialPause(ctx, cr, cred.Error())
		}
		return ctrl.Result{}, err
	}
	cc.api = r.Client
	cc.recorder = r.Recorder
	return r.reconcile(ctx, cc, req)
}

func (r *RemoteReconciler) clientsFor(ctx context.Context, cr *rolloutsv1alpha1.CompositionRollout) (clusterClients, error) {
	name, namespace := kubeconfigRef(cr, r.Namespace)
	if r.Namespace != "" && namespace != r.Namespace {
		return clusterClients{}, &credentialUnavailableError{
			message: fmt.Sprintf("kubeconfig Secret %s/%s is not in namespace %s", namespace, name, r.Namespace),
		}
	}
	env, engaged := r.Clusters.Engagement(name)
	if !engaged {
		msg := fmt.Sprintf("kubeconfig Secret %s is not engaged", name)
		if extra := credentialFailure(r.Clusters, name); extra != "" {
			msg = extra
		}
		return clusterClients{}, &credentialUnavailableError{message: msg}
	}
	if env == "" {
		return clusterClients{}, fmt.Errorf("kubeconfig Secret %s has no environment annotation", name)
	}
	cl, err := r.Clusters.Get(ctx, name)
	if err != nil {
		return clusterClients{}, fmt.Errorf("get cluster %s: %w", name, err)
	}
	cfg := cl.GetConfig()
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return clusterClients{}, fmt.Errorf("dynamic client for %s: %w", name, err)
	}
	return clusterClients{
		client:      cl.GetClient(),
		dynamic:     dyn,
		mapper:      cl.GetRESTMapper(),
		environment: env,
		cluster:     cr.Spec.Cluster,
		clientID:    name,
		cache:       cl.GetCache(),
		scheme:      cl.GetScheme(),
		clusterObj:  cl,
	}, nil
}

// SetupWithManager registers CompositionRevision and Composition watches on
// provider clusters only. CompositionRollout is watched on the hub manager.
// Call it before the manager starts, and before the hub controller's SetupWithManager.
func (r *RemoteReconciler) SetupWithManager(mgr mcmanager.Manager) error {
	if r.Inner == nil {
		return fmt.Errorf("remote reconciler has no inner CompositionRolloutReconciler")
	}
	if r.Hub == nil {
		return fmt.Errorf("remote reconciler has no hub client")
	}
	r.Inner.remote = r
	rev := &unstructured.Unstructured{}
	rev.SetGroupVersionKind(compositionRevisionGVK)
	comp := &unstructured.Unstructured{}
	comp.SetGroupVersionKind(compositionGVK)
	noLocal := mcbuilder.WithEngageWithLocalCluster(false)
	mc, err := mcbuilder.ControllerManagedBy(mgr).
		Named("compositionrollout-remote").
		Watches(rev, r.enqueueRevision, noLocal, mcbuilder.WithPredicates(createOnlyPredicate())).
		Watches(comp, r.enqueueComposition, noLocal).
		Build(r)
	if err != nil {
		return err
	}
	r.mc = mc
	return nil
}

func (r *RemoteReconciler) enqueueRevision(clusterName string, _ cluster.Cluster) handler.TypedEventHandler[client.Object, mcreconcile.Request] {
	inner := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
		compName := u.GetLabels()[xp.CompositionNameLabel]
		if compName == "" {
			return nil
		}
		return r.requests(ctx, clusterName, compName)
	})
	return mchandler.ForCluster(inner, clusterName)
}

func (r *RemoteReconciler) enqueueComposition(clusterName string, _ cluster.Cluster) handler.TypedEventHandler[client.Object, mcreconcile.Request] {
	inner := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return r.requests(ctx, clusterName, obj.GetName())
	})
	return mchandler.ForCluster(inner, clusterName)
}

func (r *RemoteReconciler) requests(ctx context.Context, clientName, compName string) []reconcile.Request {
	if r.Hub == nil || clientName == "" || compName == "" {
		return nil
	}
	var list rolloutsv1alpha1.CompositionRolloutList
	if err := r.Hub.List(ctx, &list, client.MatchingFields{FieldIndexKubeconfig: clientName}); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		item := &list.Items[i]
		if !compositionListed(item, compName) {
			continue
		}
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}})
	}
	return reqs
}

func compositionListed(cr *rolloutsv1alpha1.CompositionRollout, name string) bool {
	for i := range cr.Spec.Compositions {
		if cr.Spec.Compositions[i].Name == name {
			return true
		}
	}
	return false
}

// watchRemoteXRs registers a lazy XR watch on one engaged cluster.
// v0.20.4-alpha.7 drops engaged clusters from the controller map as soon as
// Engage returns, so MultiClusterWatch cannot see them. ForCluster plus Watch
// attaches the informer to the cluster that is reconciling now.
func (r *CompositionRolloutReconciler) watchRemoteXRs(cc clusterClients, key xrWatchKey, gvk schema.GroupVersionKind) error {
	if cc.clusterObj == nil || r.remote == nil || r.remote.mc == nil {
		return fmt.Errorf("cluster %s: XR watch has no cluster", cc.cluster)
	}
	name := cc.watchName()
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
			return r.remote.requests(ctx, clusterName, compName)
		})
		return mchandler.TypedForCluster(inner, clusterName)
	}
	src, err := mcsource.TypedKind(u, handlerFn).ForCluster(name, cc.clusterObj)
	if err != nil {
		return fmt.Errorf("xr watch for %s: %w", name, err)
	}
	if err := r.remote.mc.Watch(src); err != nil {
		return fmt.Errorf("watch %s on %s: %w", gvk, name, err)
	}
	r.xrWatches.Store(key, cc.clusterObj)
	return nil
}
