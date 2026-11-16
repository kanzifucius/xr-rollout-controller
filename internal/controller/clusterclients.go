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
// cluster empty and clusterObj nil. api reads and writes CompositionRollout.
// When api is nil, that client is client. client patches composite resources.
type clusterClients struct {
	client      client.Client
	api         client.Client
	dynamic     dynamic.Interface
	mapper      meta.RESTMapper
	recorder    record.EventRecorder
	environment string
	// cluster is the logical cluster. Empty on the local manager.
	cluster string
	// clientID is the kubeconfig Secret name in hub mode. Empty in local mode.
	clientID   string
	cache      cache.Cache
	scheme     *runtime.Scheme
	controller controller.Controller
	clusterObj cluster.Cluster
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

func (cc clusterClients) apiClient() client.Client {
	if cc.api != nil {
		return cc.api
	}
	return cc.client
}

// watchName is the engaged client id. Local mode uses the empty cluster name.
func (cc clusterClients) watchName() string {
	if cc.clientID != "" {
		return cc.clientID
	}
	return cc.cluster
}

// Placement is where the CompositionRollout reconciler runs.
type Placement int

const (
	PlacementNone Placement = iota
	PlacementLocal
	PlacementHub
)

const (
	// ModeLocal runs the CompositionRollout controller on this cluster.
	ModeLocal = "local"
	// ModeRemote appends Start only. Each cluster runs its own controller.
	ModeRemote = "remote"
	// ModeHub keeps CompositionRollout on this install and patches spoke XRs.
	ModeHub = "hub"
)

// PlacementFor decides where CompositionRollout is reconciled.
// Hub mode does not also start the local controller.
// Remote mode does not reconcile CompositionRollout on this install.
func PlacementFor(enableRollout bool, mode string) Placement {
	switch mode {
	case ModeHub:
		return PlacementHub
	case ModeRemote:
		return PlacementNone
	case ModeLocal:
		if enableRollout {
			return PlacementLocal
		}
		return PlacementNone
	default:
		return PlacementNone
	}
}
