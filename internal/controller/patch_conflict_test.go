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
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCrossplaneRevisionConflict(t *testing.T) {
	t.Parallel()
	gr := schema.GroupResource{Group: "e2e.platform.example.org", Resource: "xnops"}
	crossplane := apierrors.NewConflict(gr, "a", fmt.Errorf(
		`Apply failed with 1 conflict: conflict with "crossplane" using e2e.platform.example.org/v1alpha1: .spec.crossplane.compositionRevisionRef.name`,
	))
	if !crossplaneRevisionConflict(crossplane) {
		t.Fatalf("crossplane conflict = false, err = %v", crossplane)
	}

	composite := apierrors.NewConflict(gr, "a", fmt.Errorf(
		`Apply failed with 1 conflict: conflict with "apiextensions.crossplane.io/composite": .spec.crossplane.compositionRevisionRef.name`,
	))
	if !crossplaneRevisionConflict(composite) {
		t.Fatalf("composite conflict = false, err = %v", composite)
	}

	gitops := apierrors.NewConflict(gr, "a", fmt.Errorf(
		`Apply failed with 1 conflict: conflict with "gitops": .spec.crossplane.compositionRevisionRef.name`,
	))
	if crossplaneRevisionConflict(gitops) {
		t.Fatalf("gitops conflict = true, err = %v", gitops)
	}

	both := apierrors.NewConflict(gr, "a", fmt.Errorf(
		`Apply failed with 2 conflicts: conflict with "crossplane" and conflict with "gitops"`,
	))
	if crossplaneRevisionConflict(both) {
		t.Fatalf("mixed conflict = true, err = %v", both)
	}

	if crossplaneRevisionConflict(fmt.Errorf("conflict with %q", "crossplane")) {
		t.Fatal("non-status error was treated as a Crossplane conflict")
	}
}
