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
	"time"
)

func TestEvaluateSoakHealthyStable(t *testing.T) {
	soakStart := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	before := soakStart.Add(-time.Minute)

	items := []HealthItem{
		{ReadyTrue: true, SyncedTrue: true, ReadySince: before, SyncedSince: before},
		{ReadyTrue: true, SyncedTrue: true, ReadySince: before, SyncedSince: before},
	}

	healthy, unhealthy, failed := EvaluateSoak(items, soakStart, soakStart.Add(time.Hour), 0)
	if failed || unhealthy != 0 || healthy != 2 {
		t.Fatalf("got healthy=%d unhealthy=%d failed=%v", healthy, unhealthy, failed)
	}
}

func TestEvaluateSoakMidSoakFlipFails(t *testing.T) {
	soakStart := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	before := soakStart.Add(-time.Minute)

	items := []HealthItem{
		{ReadyTrue: false, SyncedTrue: true, ReadySince: before, SyncedSince: before},
	}

	_, unhealthy, failed := EvaluateSoak(items, soakStart, soakStart.Add(time.Minute), 1)
	if !failed || unhealthy != 1 {
		t.Fatalf("expected flap failure, got unhealthy=%d failed=%v", unhealthy, failed)
	}
}

func TestEvaluateSoakNotYetStableCountsUnhealthy(t *testing.T) {
	soakStart := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	after := soakStart.Add(time.Minute)

	items := []HealthItem{
		{ReadyTrue: true, SyncedTrue: true, ReadySince: after, SyncedSince: after},
	}

	healthy, unhealthy, failed := EvaluateSoak(items, soakStart, soakStart.Add(time.Hour), 0)
	if failed || healthy != 0 || unhealthy != 0 {
		t.Fatalf("expected stabilizing (not yet healthy), got healthy=%d unhealthy=%d failed=%v", healthy, unhealthy, failed)
	}
}

func TestEvaluateSoakMaxUnhealthy(t *testing.T) {
	soakStart := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	before := soakStart.Add(-time.Minute)

	items := []HealthItem{
		{ReadyTrue: false, SyncedTrue: true, ReadySince: before, SyncedSince: before},
		{ReadyTrue: false, SyncedTrue: true, ReadySince: before, SyncedSince: before},
	}

	_, unhealthy, failed := EvaluateSoak(items, soakStart, soakStart.Add(time.Hour), 1)
	if !failed || unhealthy != 2 {
		t.Fatalf("expected failed when unhealthy exceeds maxUnhealthy, got unhealthy=%d failed=%v", unhealthy, failed)
	}
}
