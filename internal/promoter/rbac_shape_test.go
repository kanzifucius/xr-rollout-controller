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

package promoter

import (
	"os"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestRemoteRoleDoesNotGrantXROrStatusWrite(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../config/rbac/promoter_remote_role.yaml")
	if err != nil {
		t.Fatalf("read role: %v", err)
	}
	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(raw, &role); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, rule := range role.Rules {
		for _, res := range rule.Resources {
			if res == "compositionrollouts/status" {
				for _, verb := range rule.Verbs {
					if verb == "update" || verb == "patch" || verb == "create" || verb == "*" {
						t.Fatalf("status verbs = %v", rule.Verbs)
					}
				}
			}
			if res == "*" || res == "compositeresources" {
				t.Fatalf("resource %s", res)
			}
		}
	}
}
