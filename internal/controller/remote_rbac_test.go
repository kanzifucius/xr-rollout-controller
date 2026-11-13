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
	"os"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestRemoteReconcileRoleGrantsStatusAndNotXRWildcards(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../config/rbac/remote_reconcile_role.yaml")
	if err != nil {
		t.Fatalf("read role: %v", err)
	}
	docs := strings.Split(string(raw), "\n---\n")
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal([]byte(docs[0]), &role); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if role.Name != "xr-rollout-remote" {
		t.Fatalf("name = %s", role.Name)
	}
	var statusWrite, triggerPatch, compositionRead bool
	for _, rule := range role.Rules {
		for _, res := range rule.Resources {
			if res == "*" || strings.Contains(res, "composite") {
				t.Fatalf("resource %s", res)
			}
			if res == "compositionrollouts/status" {
				for _, verb := range rule.Verbs {
					if verb == "update" || verb == "patch" {
						statusWrite = true
					}
				}
			}
			if res == "compositionrollouts" {
				for _, verb := range rule.Verbs {
					if verb == "create" || verb == "delete" || verb == "*" {
						t.Fatalf("compositionrollouts verb %s", verb)
					}
					if verb == "patch" {
						triggerPatch = true
					}
				}
			}
			if res == "compositions" || res == "compositionrevisions" {
				compositionRead = true
			}
			if res == "secrets" {
				t.Fatal("spoke role must not grant secrets")
			}
		}
	}
	if !statusWrite || !triggerPatch || !compositionRead {
		t.Fatalf("status %v patch %v compositions %v", statusWrite, triggerPatch, compositionRead)
	}
	if strings.Count(string(raw), "\naggregationRule:") != 1 {
		t.Fatalf("aggregationRule count = %d", strings.Count(string(raw), "\naggregationRule:"))
	}
	if strings.Contains(string(raw), "metadata:\n  aggregationRule:") {
		t.Fatal("aggregationRule nested under metadata")
	}
}
