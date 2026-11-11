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
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kanzifucius/xr-rollout-controller/internal/xp"
)

func TestRevisionRefApplyConfigurationIsMinimal(t *testing.T) {
	src := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.org/v1",
		"kind":       "XDatabase",
		"metadata": map[string]interface{}{
			"name":            "db",
			"namespace":       "prod",
			"resourceVersion": "9",
			"uid":             "abc",
			"labels":          map[string]interface{}{"app": "db"},
		},
		"spec": map[string]interface{}{
			"teamField": "keep",
			"crossplane": map[string]interface{}{
				"compositionRef":          map[string]interface{}{"name": "comp"},
				"compositionUpdatePolicy": "Manual",
				"compositionRevisionRef":  map[string]interface{}{"name": "old"},
			},
		},
		"status": map[string]interface{}{"ready": true},
	}}

	got := revisionRefApplyConfiguration(src, "comp-new")
	if got.GetAPIVersion() != "example.org/v1" || got.GetKind() != "XDatabase" {
		t.Fatalf("identity: %s %s", got.GetAPIVersion(), got.GetKind())
	}
	if got.GetName() != "db" || got.GetNamespace() != "prod" {
		t.Fatalf("meta: %s/%s", got.GetNamespace(), got.GetName())
	}
	if xp.GetCompositionRevisionRef(got) != "comp-new" {
		t.Fatalf("revision ref: %q", xp.GetCompositionRevisionRef(got))
	}
	if _, found, _ := unstructured.NestedString(got.Object, "metadata", "resourceVersion"); found {
		t.Fatal("apply configuration includes resourceVersion")
	}
	if _, found, _ := unstructured.NestedString(got.Object, "metadata", "uid"); found {
		t.Fatal("apply configuration includes uid")
	}
	if labels := got.GetLabels(); len(labels) != 0 {
		t.Fatalf("apply configuration includes labels: %v", labels)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "status"); found {
		t.Fatal("apply configuration includes status")
	}
	if _, found, _ := unstructured.NestedString(got.Object, "spec", "teamField"); found {
		t.Fatal("apply configuration includes spec.teamField")
	}
	if _, found, _ := unstructured.NestedString(got.Object, "spec", "crossplane", "compositionRef", "name"); found {
		t.Fatal("apply configuration includes compositionRef")
	}
	if _, found, _ := unstructured.NestedString(got.Object, "spec", "crossplane", "compositionUpdatePolicy"); found {
		t.Fatal("apply configuration includes compositionUpdatePolicy")
	}

	spec, found, _ := unstructured.NestedMap(got.Object, "spec")
	if !found || len(spec) != 1 {
		t.Fatalf("spec should contain only crossplane, got %#v", spec)
	}
}

func TestRevisionRefApplyConfigurationOmitsEmptyNamespace(t *testing.T) {
	src := &unstructured.Unstructured{}
	src.SetAPIVersion("example.org/v1")
	src.SetKind("XClusterDatabase")
	src.SetName("db")

	got := revisionRefApplyConfiguration(src, "rev-1")
	if got.GetNamespace() != "" {
		t.Fatalf("namespace: %q", got.GetNamespace())
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(got.Object, "metadata", "namespace"); found {
		t.Fatal("cluster-scoped apply configuration sets namespace")
	}
}

func TestSSAConflictingManagers(t *testing.T) {
	conflict := apierrors.NewApplyConflict([]metav1.StatusCause{{
		Type:    metav1.CauseTypeFieldManagerConflict,
		Message: `conflict with "argocd"`,
		Field:   ".spec.crossplane.compositionRevisionRef.name",
	}}, `Apply failed with 1 conflict: conflict with "argocd": .spec.crossplane.compositionRevisionRef.name`)

	managers, ok := ssaConflictingManagers(conflict)
	if !ok {
		t.Fatal("expected SSA conflict")
	}
	if len(managers) != 1 || managers[0] != "argocd" {
		t.Fatalf("managers: %v", managers)
	}
	if msg := patchConflictMessage(managers); !strings.Contains(msg, "argocd") || !strings.Contains(msg, compositionRevisionRefField) {
		t.Fatalf("message: %q", msg)
	}

	multi := apierrors.NewApplyConflict([]metav1.StatusCause{
		{Type: metav1.CauseTypeFieldManagerConflict, Message: `conflict with "argocd"`, Field: ".spec.crossplane.compositionRevisionRef.name"},
		{Type: metav1.CauseTypeFieldManagerConflict, Message: `conflict with "flux"`, Field: ".spec.crossplane.compositionRevisionRef.name"},
	}, "Apply failed with 2 conflicts: conflicts with \"argocd\":\n- .spec.crossplane.compositionRevisionRef.name\nconflicts with \"flux\":\n- .spec.crossplane.compositionRevisionRef.name")
	managers, ok = ssaConflictingManagers(multi)
	if !ok || len(managers) != 2 || managers[0] != "argocd" || managers[1] != "flux" {
		t.Fatalf("multi managers: ok=%v %v", ok, managers)
	}

	optimistic := apierrors.NewConflict(schema.GroupResource{Group: "example.org", Resource: "xdatabases"}, "db", errModified())
	if _, ok := ssaConflictingManagers(optimistic); ok {
		t.Fatal("optimistic concurrency conflict must keep retrying")
	}
}

type modifiedError struct{}

func (modifiedError) Error() string {
	return "the object has been modified; please apply your changes to the latest version and try again"
}

func errModified() error { return modifiedError{} }
