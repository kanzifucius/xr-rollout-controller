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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

const defaultFieldManager = "xr-rollout-controller"

// CompositionRolloutReconciler reconciles a CompositionRollout object.
type CompositionRolloutReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Dynamic  dynamic.Interface
	Recorder record.EventRecorder
	Mapper   meta.RESTMapper

	Environment          string
	ForceOwnership       bool
	MetricsAggregateOnly bool
	FieldManager         string

	mgr        ctrl.Manager
	controller controller.Controller
	remote     *RemoteReconciler
	xrWatchMu  sync.Mutex
	xrWatches  sync.Map
}

// +kubebuilder:rbac:groups=rollouts.platform.example.org,resources=compositionrollouts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rollouts.platform.example.org,resources=compositionrollouts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=rollouts.platform.example.org,resources=compositionrollouts/finalizers,verbs=update
// +kubebuilder:rbac:groups=apiextensions.crossplane.io,resources=compositionrevisions,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.crossplane.io,resources=compositions,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile moves cluster state toward the desired CompositionRollout spec.
// With a remote reconciler attached, the object lives on the hub and spec.cluster
// selects the spoke whose composite resources are patched.
func (r *CompositionRolloutReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.remote != nil {
		return r.reconcileHub(ctx, req)
	}
	return r.reconcile(ctx, localClients(r), req)
}

func (r *CompositionRolloutReconciler) reconcile(ctx context.Context, cc clusterClients, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cr := &rolloutsv1alpha1.CompositionRollout{}
	if err := cc.apiClient().Get(ctx, req.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	overlap, message, err := r.detectOverlap(ctx, cc, cr)
	if err != nil {
		return ctrl.Result{}, err
	}
	if overlap {
		cr.Status.ObservedGeneration = cr.Generation
		for i := range cr.Spec.Compositions {
			st := findCompositionState(&cr.Status, cr.Spec.Compositions[i].Name)
			st.Phase = rolloutsv1alpha1.PhasePaused
			st.PauseReason = &rolloutsv1alpha1.PauseReason{
				Reason:  rolloutsv1alpha1.PauseReasonOverlap,
				Message: message,
			}
		}
		updateRolloutConditions(cr)
		if err := cc.apiClient().Status().Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		cc.recorder.Eventf(cr, "Warning", "RolloutPaused", "%s", message)
		return ctrl.Result{RequeueAfter: periodicRequeue}, nil
	}

	if cr.Spec.Suspend {
		for i := range cr.Spec.Compositions {
			st := findCompositionState(&cr.Status, cr.Spec.Compositions[i].Name)
			st.Phase = rolloutsv1alpha1.PhasePaused
			st.PauseReason = &rolloutsv1alpha1.PauseReason{
				Reason:  rolloutsv1alpha1.PauseReasonSuspended,
				Message: "spec.suspend is true",
			}
		}
	}

	minRequeue := periodicRequeue
	for i := range cr.Spec.Compositions {
		target := cr.Spec.Compositions[i]
		state := findCompositionState(&cr.Status, target.Name)
		if state.Phase == "" {
			state.Phase = rolloutsv1alpha1.PhaseIdle
		}

		compRes, err := r.reconcileComposition(ctx, cc, cr, target, state)
		if err != nil {
			logger.Error(err, "reconcile composition", "composition", target.Name)
			return ctrl.Result{}, err
		}
		if compRes.requeueAfter > 0 && compRes.requeueAfter < minRequeue {
			minRequeue = compRes.requeueAfter
		}
	}

	cr.Status.ObservedGeneration = cr.Generation
	updateRolloutConditions(cr)
	if err := cc.apiClient().Status().Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: minRequeue}, nil
}

func (r *CompositionRolloutReconciler) detectOverlap(ctx context.Context, cc clusterClients, cr *rolloutsv1alpha1.CompositionRollout) (bool, string, error) {
	var all rolloutsv1alpha1.CompositionRolloutList
	if err := cc.apiClient().List(ctx, &all); err != nil {
		return false, "", err
	}
	message := overlapMessage(cr, all.Items, cc.cluster)
	if message == "" {
		return false, "", nil
	}
	return true, message, nil
}

// SetupWithManager registers the reconciler with the Manager.
func (r *CompositionRolloutReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.FieldManager == "" {
		r.FieldManager = defaultFieldManager
	}
	if err := setupIndexes(mgr); err != nil {
		return err
	}

	r.mgr = mgr
	r.Mapper = mgr.GetRESTMapper()

	bld := ctrl.NewControllerManagedBy(mgr).
		For(&rolloutsv1alpha1.CompositionRollout{}).
		Named("compositionrollout")
	// Remote mode watches Compositions and composite resources on the spokes.
	// The hub manager only needs the CompositionRollout informer.
	if r.remote == nil {
		bld = r.setupStaticWatches(bld)
	}

	ctr, err := bld.Build(r)
	if err != nil {
		return err
	}
	r.controller = ctr
	return nil
}
