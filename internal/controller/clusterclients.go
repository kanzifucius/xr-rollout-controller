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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

// clusterClients is the API surface for one cluster. The local manager leaves
// cluster empty and clusterObj nil.
type clusterClients struct {
	client      client.Client
	dynamic     dynamic.Interface
	mapper      meta.RESTMapper
	recorder    record.EventRecorder
	environment string
	cluster     string
	cache       cache.Cache
	scheme      *runtime.Scheme
	controller  controller.Controller
	clusterObj  cluster.Cluster
}

func localClients(r *CompositionRolloutReconciler) clusterClients {
	cc := clusterClients{
		client:      r.Client,
		dynamic:     r.Dynamic,
		mapper:      r.Mapper,
		recorder:    r.Recorder,
		environment: r.Environment,
		scheme:      r.Scheme,
		controller:  r.controller,
	}
	if r.mgr != nil {
		cc.cache = r.mgr.GetCache()
	}
	return cc
}

// Placement is where the CompositionRollout reconciler runs.
type Placement int

const (
	PlacementNone Placement = iota
	PlacementLocal
	PlacementRemote
)

// PlacementFor decides the rollout controller from the process flags.
// Remote reconcile replaces the local controller even when both flags are set.
func PlacementFor(enableRollout, remoteReconcile bool) Placement {
	if remoteReconcile {
		return PlacementRemote
	}
	if enableRollout {
		return PlacementLocal
	}
	return PlacementNone
}
