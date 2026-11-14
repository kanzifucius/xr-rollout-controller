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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/cluster"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

type stubClusters struct {
	gets     int
	last     string
	env      string
	engaged  bool
	failures map[string]string
}

func (s *stubClusters) Get(_ context.Context, name string) (cluster.Cluster, error) {
	s.gets++
	s.last = name
	return nil, fmt.Errorf("Get should not be called")
}

func (s *stubClusters) Engagement(name string) (string, bool) {
	if s.failures != nil {
		if _, ok := s.failures[name]; ok {
			return "", false
		}
	}
	if !s.engaged {
		return "", false
	}
	return s.env, true
}

func (s *stubClusters) CredentialFailure(name string) string {
	if s.failures == nil {
		return ""
	}
	return s.failures[name]
}

func TestClientsForMissingEnvironmentDoesNotTouchTheCluster(t *testing.T) {
	t.Parallel()
	stub := &stubClusters{engaged: true}
	r := &RemoteReconciler{Clusters: stub, Namespace: "xr-rollout"}
	_, err := r.clientsFor(context.Background(), rolloutForCluster("prod"))
	if err == nil {
		t.Fatal("expected error")
	}
	var cred *credentialUnavailableError
	if errors.As(err, &cred) {
		t.Fatal("missing environment was treated as a missing secret")
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
}

func TestClientsForRejectsAnEmptyAnnotation(t *testing.T) {
	t.Parallel()
	stub := &stubClusters{engaged: true, env: ""}
	r := &RemoteReconciler{Clusters: stub, Namespace: "xr-rollout"}
	_, err := r.clientsFor(context.Background(), rolloutForCluster("acc"))
	if err == nil {
		t.Fatal("expected error")
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
}

func rolloutForCluster(cluster string) *rolloutsv1alpha1.CompositionRollout {
	cr := &rolloutsv1alpha1.CompositionRollout{}
	cr.Name = "payments"
	cr.Spec.Cluster = cluster
	cr.Spec.Compositions = []rolloutsv1alpha1.CompositionTarget{{Name: "payments.example.org"}}
	return cr
}
