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
	"testing"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

func rolloutNamed(name, cluster, composition string) rolloutsv1alpha1.CompositionRollout {
	cr := rolloutsv1alpha1.CompositionRollout{}
	cr.Name = name
	cr.Spec.Cluster = cluster
	if composition != "" {
		cr.Spec.Compositions = []rolloutsv1alpha1.CompositionTarget{{Name: composition}}
	}
	return cr
}

func TestOverlapMessageScopesHubObjectsToOneCluster(t *testing.T) {
	t.Parallel()
	self := rolloutNamed("payments-acc", "acc", "db")
	others := []rolloutsv1alpha1.CompositionRollout{
		rolloutNamed("payments-dev", "dev", "db"),
		self,
	}
	if got := overlapMessage(&self, others, "acc"); got != "" {
		t.Fatalf("overlap = %q", got)
	}
	others = append(others, rolloutNamed("payments-acc-2", "acc", "cache"))
	got := overlapMessage(&self, others, "acc")
	if got == "" {
		t.Fatal("expected overlap")
	}
	local := overlapMessage(&self, []rolloutsv1alpha1.CompositionRollout{
		rolloutNamed("other", "dev", "db"),
	}, "")
	if local == "" {
		t.Fatal("expected local composition overlap")
	}
}
