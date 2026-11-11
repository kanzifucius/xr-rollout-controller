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

package rollout

import (
	"testing"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func mkCandidates() []Candidate {
	return []Candidate{
		{Namespace: "prod", Name: "a", UID: "uid-prod-a"},
		{Namespace: "prod", Name: "b", UID: "uid-prod-b"},
		{Namespace: "staging", Name: "a", UID: "uid-staging-a"},
		{Namespace: "dev", Name: "a", UID: "uid-dev-a"},
		{Namespace: "dev", Name: "b", UID: "uid-dev-b"},
	}
}

func TestSelectCanariesCountStable(t *testing.T) {
	candidates := mkCandidates()
	count := int32(3)

	first, err := SelectCanaries(candidates, rolloutsv1alpha1.CanaryStrategyCount, &count, nil, nil, nil)
	if err != nil {
		t.Fatalf("SelectCanaries: %v", err)
	}
	second, err := SelectCanaries(candidates, rolloutsv1alpha1.CanaryStrategyCount, &count, nil, nil, nil)
	if err != nil {
		t.Fatalf("SelectCanaries: %v", err)
	}

	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("expected 3 canaries, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("selection not stable at %d: %#v vs %#v", i, first[i], second[i])
		}
	}
}

func TestSelectCanariesPreferredNamespacesOrder(t *testing.T) {
	candidates := mkCandidates()
	count := int32(4)
	preferred := []string{"staging", "prod"}

	got, err := SelectCanaries(candidates, rolloutsv1alpha1.CanaryStrategyCount, &count, nil, preferred, nil)
	if err != nil {
		t.Fatalf("SelectCanaries: %v", err)
	}

	// All staging and prod should appear before dev.
	lastPreferred := -1
	firstRest := len(got)
	for i, c := range got {
		if c.Namespace == "staging" || c.Namespace == "prod" {
			lastPreferred = i
		}
		if c.Namespace == "dev" && firstRest == len(got) {
			firstRest = i
		}
	}
	if lastPreferred >= firstRest {
		t.Fatalf("preferred namespaces not ordered first: %+v", got)
	}

	// Within staging bucket only one candidate.
	if got[0].Namespace != "staging" {
		t.Fatalf("expected staging first, got %+v", got[0])
	}
}

func TestSelectCanariesPercentMinimumOne(t *testing.T) {
	candidates := mkCandidates()
	pct := int32(1)

	got, err := SelectCanaries(candidates, rolloutsv1alpha1.CanaryStrategyPercent, nil, &pct, nil, nil)
	if err != nil {
		t.Fatalf("SelectCanaries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 canary at 1%%, got %d", len(got))
	}
}

func TestSelectCanariesLabelSelector(t *testing.T) {
	candidates := mkCandidates()
	got, err := SelectCanaries(
		candidates,
		rolloutsv1alpha1.CanaryStrategyLabelSelector,
		nil,
		nil,
		nil,
		func(c Candidate) bool { return c.Namespace == "prod" },
	)
	if err != nil {
		t.Fatalf("SelectCanaries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 prod canaries, got %d", len(got))
	}
}
