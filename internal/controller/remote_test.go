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
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

type stubClusters struct {
	gets int
	env  string
	ok   bool
}

func (s *stubClusters) Get(context.Context, string) (cluster.Cluster, error) {
	s.gets++
	return nil, fmt.Errorf("Get should not be called")
}

func (s *stubClusters) Environment(string) (string, bool) {
	return s.env, s.ok
}

func TestClientsForMissingEnvironmentDoesNotTouchTheCluster(t *testing.T) {
	t.Parallel()
	stub := &stubClusters{}
	r := &RemoteReconciler{Clusters: stub}
	_, err := r.clientsFor(context.Background(), "prod")
	if err == nil {
		t.Fatal("expected error")
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
}

func TestClientsForRejectsAnEmptyAnnotation(t *testing.T) {
	t.Parallel()
	stub := &stubClusters{ok: true, env: ""}
	r := &RemoteReconciler{Clusters: stub}
	_, err := r.clientsFor(context.Background(), "acc")
	if err == nil {
		t.Fatal("expected error")
	}
	if stub.gets != 0 {
		t.Fatalf("Get calls = %d", stub.gets)
	}
}
