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
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigsyaml "sigs.k8s.io/yaml"
)

const aggregateToControllerLabel = "rollouts.platform.example.org/aggregate-to-rollout-controller"

func TestAggregateXRRoleIsBoundToControllerServiceAccount(t *testing.T) {
	role := mustSingleYAML(t, filepath.Join("..", "..", "config", "rbac", "xr_aggregate_role.yaml"))
	binding := mustSingleYAML(t, filepath.Join("..", "..", "config", "rbac", "xr_aggregate_role_binding.yaml"))
	managerBinding := mustSingleYAML(t, filepath.Join("..", "..", "config", "rbac", "role_binding.yaml"))
	sa := mustSingleYAML(t, filepath.Join("..", "..", "config", "rbac", "service_account.yaml"))

	if role.GetKind() != "ClusterRole" || role.GetName() != "xr-rollout-controller-xr-access" {
		t.Fatalf("aggregate role identity: %s %s", role.GetKind(), role.GetName())
	}
	rules, found, err := unstructured.NestedSlice(role.Object, "rules")
	if err != nil {
		t.Fatal(err)
	}
	if found && len(rules) != 0 {
		t.Fatalf("aggregate role must not embed rules, got %#v", rules)
	}
	selectors, found, err := unstructured.NestedSlice(role.Object, "aggregationRule", "clusterRoleSelectors")
	if err != nil || !found || len(selectors) != 1 {
		t.Fatalf("aggregation selectors: found=%v err=%v %#v", found, err, selectors)
	}
	selector, ok := selectors[0].(map[string]interface{})
	if !ok {
		t.Fatalf("selector type %T", selectors[0])
	}
	label, found, err := unstructured.NestedString(selector, "matchLabels", aggregateToControllerLabel)
	if err != nil || !found || label != "true" {
		t.Fatalf("aggregation label: found=%v err=%v value=%q", found, err, label)
	}

	if binding.GetKind() != "ClusterRoleBinding" {
		t.Fatalf("binding kind %s", binding.GetKind())
	}
	roleName, _, _ := unstructured.NestedString(binding.Object, "roleRef", "name")
	if roleName != role.GetName() {
		t.Fatalf("binding roleRef %q, role %q", roleName, role.GetName())
	}

	bindKind, bindName, bindNS := firstSubject(t, binding)
	saKind, saName, saNS := "ServiceAccount", sa.GetName(), sa.GetNamespace()
	mgrKind, mgrName, mgrNS := firstSubject(t, managerBinding)
	if bindKind != saKind || bindName != saName || bindNS != saNS {
		t.Fatalf("binding subject %s %s/%s, service account %s %s/%s", bindKind, bindNS, bindName, saKind, saNS, saName)
	}
	if bindKind != mgrKind || bindName != mgrName || bindNS != mgrNS {
		t.Fatalf("XR binding subject %s %s/%s differs from manager binding %s %s/%s", bindKind, bindNS, bindName, mgrKind, mgrNS, mgrName)
	}

	kustomize, err := os.ReadFile(filepath.Join("..", "..", "config", "rbac", "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !kustomizeListsResource(kustomize, "xr_aggregate_role.yaml") || !kustomizeListsResource(kustomize, "xr_aggregate_role_binding.yaml") {
		t.Fatalf("kustomization does not install the aggregate role and its binding:\n%s", kustomize)
	}
}

func firstSubject(t *testing.T, binding *unstructured.Unstructured) (kind, name, namespace string) {
	t.Helper()
	subjects, found, err := unstructured.NestedSlice(binding.Object, "subjects")
	if err != nil || !found || len(subjects) != 1 {
		t.Fatalf("subjects: found=%v err=%v %#v", found, err, subjects)
	}
	subject, ok := subjects[0].(map[string]interface{})
	if !ok {
		t.Fatalf("subject type %T", subjects[0])
	}
	kind, _, _ = unstructured.NestedString(subject, "kind")
	name, _, _ = unstructured.NestedString(subject, "name")
	namespace, _, _ = unstructured.NestedString(subject, "namespace")
	return kind, name, namespace
}

func kustomizeListsResource(raw []byte, name string) bool {
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "- "+name {
			return true
		}
	}
	return false
}

func mustSingleYAML(t *testing.T, path string) *unstructured.Unstructured {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := decodeYAMLObjects(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("%s: expected 1 document, got %d", path, len(docs))
	}
	return docs[0]
}

func decodeYAMLObjects(raw []byte) ([]*unstructured.Unstructured, error) {
	parts := bytes.Split(raw, []byte("\n---\n"))
	docs := make([]*unstructured.Unstructured, 0, len(parts))
	for _, part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) == 0 {
			continue
		}
		jsonBytes, err := sigsyaml.YAMLToJSON(part)
		if err != nil {
			return nil, err
		}
		if len(jsonBytes) == 0 || string(jsonBytes) == "null" || string(jsonBytes) == "{}" {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonBytes); err != nil {
			return nil, err
		}
		if u.GetAPIVersion() == "" || u.GetKind() == "" {
			continue
		}
		docs = append(docs, u)
	}
	return docs, nil
}
