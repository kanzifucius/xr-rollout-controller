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

func TestHubSpokeRoleReadsCompositionsAndDoesNotGrantRollouts(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../config/rbac/hub_spoke_role.yaml")
	if err != nil {
		t.Fatalf("read role: %v", err)
	}
	docs := strings.Split(string(raw), "\n---\n")
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal([]byte(docs[0]), &role); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if role.Name != "xr-rollout-hub" {
		t.Fatalf("name = %s", role.Name)
	}
	var compositionRead bool
	for _, rule := range role.Rules {
		for _, res := range rule.Resources {
			if res == "*" || strings.Contains(res, "composite") || res == "compositionrollouts" || res == "compositionrollouts/status" {
				t.Fatalf("resource %s", res)
			}
			if res == "compositions" || res == "compositionrevisions" {
				compositionRead = true
			}
			if res == "secrets" {
				t.Fatal("spoke role must not grant secrets")
			}
		}
	}
	if !compositionRead {
		t.Fatal("missing composition read")
	}
	if strings.Count(string(raw), "\naggregationRule:") != 1 {
		t.Fatalf("aggregationRule count = %d", strings.Count(string(raw), "\naggregationRule:"))
	}
	if strings.Contains(string(raw), "metadata:\n  aggregationRule:") {
		t.Fatal("aggregationRule nested under metadata")
	}
	if !strings.Contains(string(raw), `rollouts.platform.example.org/aggregate-to-rollout-controller: "true"`) {
		t.Fatal("missing aggregate label")
	}
	if !strings.Contains(string(raw), "name: xr-rollout-hub-xr-access") {
		t.Fatal("missing aggregate role")
	}
}
