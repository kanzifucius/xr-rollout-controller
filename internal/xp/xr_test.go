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

package xp

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGettersAndSetters(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"crossplane": map[string]interface{}{
				"compositionRef": map[string]interface{}{
					"name": "comp-a",
				},
				"compositionUpdatePolicy": "Manual",
			},
		},
	}}

	if got := GetCompositionRefName(u); got != "comp-a" {
		t.Fatalf("compositionRef: got %q", got)
	}
	if got := GetCompositionUpdatePolicy(u); got != compositionUpdatePolicyManual {
		t.Fatalf("updatePolicy: got %q", got)
	}

	SetCompositionRevisionRef(u, "rev-1")
	if got := GetCompositionRevisionRef(u); got != "rev-1" {
		t.Fatalf("revisionRef: got %q", got)
	}

	u.Object = map[string]interface{}{"spec": map[string]interface{}{}}
	if got := GetCompositionUpdatePolicy(u); got != compositionUpdatePolicyAutomatic {
		t.Fatalf("default policy: got %q", got)
	}
}

func TestIsReadyAndSynced(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"type":   "Ready",
					"status": "True",
				},
				map[string]interface{}{
					"type":   "Synced",
					"status": "False",
				},
			},
		},
	}}
	ok, ready, synced := IsReadyAndSynced(u)
	if ok || ready == nil || synced == nil {
		t.Fatalf("expected not healthy, got ok=%v", ok)
	}
}

func TestMatchesXRSelector(t *testing.T) {
	u := &unstructured.Unstructured{}
	u.SetNamespace("prod")
	u.SetLabels(map[string]string{"env": "prod", "tier": "app"})

	sel := &metav1.LabelSelector{MatchLabels: map[string]string{"env": "prod"}}
	if !MatchesXRSelector(u, []string{"prod"}, sel) {
		t.Fatal("expected match")
	}
	if MatchesXRSelector(u, []string{"dev"}, sel) {
		t.Fatal("expected namespace mismatch")
	}
	if MatchesXRSelector(u, nil, &metav1.LabelSelector{MatchLabels: map[string]string{"env": "dev"}}) {
		t.Fatal("expected label mismatch")
	}
}

func TestParseTimeViaCondition(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339)
	m := map[string]interface{}{
		"type":               "Ready",
		"status":             "True",
		"lastTransitionTime": ts,
	}
	cond := conditionFromMap(m)
	if cond.LastTransitionTime.IsZero() {
		t.Fatal("expected parsed time")
	}
}
