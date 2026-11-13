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

package metrics

import "testing"

func TestLabelsKeepLocalSeriesAndSeparateClusters(t *testing.T) {
	t.Parallel()
	rollout, composition := labels("", "team-payments", "platformdb.example.org", false)
	if rollout != "team-payments" || composition != "platformdb.example.org" {
		t.Fatalf("local = %s, %s", rollout, composition)
	}
	rollout, composition = labels("", "team-payments", "platformdb.example.org", true)
	if rollout != aggregateAll || composition != aggregateAll {
		t.Fatalf("local aggregate = %s, %s", rollout, composition)
	}
	rollout, composition = labels("prod", "team-payments", "platformdb.example.org", false)
	if rollout != "prod/team-payments" || composition != "platformdb.example.org" {
		t.Fatalf("remote = %s, %s", rollout, composition)
	}
	rollout, composition = labels("prod", "team-payments", "platformdb.example.org", true)
	if rollout != "prod/"+aggregateAll || composition != aggregateAll {
		t.Fatalf("remote aggregate = %s, %s", rollout, composition)
	}
}
