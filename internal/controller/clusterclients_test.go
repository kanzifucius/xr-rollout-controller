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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

type patchRecorder struct {
	client.Client
	patches int
	obj     client.Object
}

func (p *patchRecorder) Patch(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
	p.patches++
	p.obj = obj.DeepCopyObject().(client.Object)
	return nil
}

func TestPatchXRRevisionUsesTheClusterClient(t *testing.T) {
	t.Parallel()
	rec := &patchRecorder{}
	r := &CompositionRolloutReconciler{FieldManager: defaultFieldManager}
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("example.org/v1")
	u.SetKind("XDatabase")
	u.SetNamespace("payments")
	u.SetName("orders")
	err := r.patchXRRevision(context.Background(), clusterClients{client: rec}, u, "rev-9")
	if err != nil {
		t.Fatalf("patchXRRevision: %v", err)
	}
	if rec.patches != 1 {
		t.Fatalf("patches = %d, want 1", rec.patches)
	}
	patched, ok := rec.obj.(*unstructured.Unstructured)
	if !ok {
		t.Fatalf("patched type %T", rec.obj)
	}
	if xp.GetCompositionRevisionRef(patched) != "rev-9" {
		t.Fatalf("revision = %s", xp.GetCompositionRevisionRef(patched))
	}
	if _, found, _ := unstructured.NestedString(patched.Object, "spec", "crossplane", "compositionUpdatePolicy"); found {
		t.Fatalf("patch set compositionUpdatePolicy")
	}
	if _, found, _ := unstructured.NestedString(patched.Object, "spec", "compositionUpdatePolicy"); found {
		t.Fatalf("patch set compositionUpdatePolicy")
	}
}

func TestPlacementFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		enable, remote bool
		want           Placement
	}{
		{enable: true, want: PlacementLocal},
		{want: PlacementNone},
		{remote: true, want: PlacementRemote},
		{enable: true, remote: true, want: PlacementRemote},
	}
	for _, tc := range cases {
		got := PlacementFor(tc.enable, tc.remote)
		if got != tc.want {
			t.Fatalf("PlacementFor(%v, %v) = %v, want %v", tc.enable, tc.remote, got, tc.want)
		}
	}
}
