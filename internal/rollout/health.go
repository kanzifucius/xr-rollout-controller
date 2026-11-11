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

import "time"

// HealthItem carries Ready/Synced state for soak evaluation.
type HealthItem struct {
	ReadyTrue   bool
	SyncedTrue  bool
	ReadySince  time.Time
	SyncedSince time.Time
}

// EvaluateSoak counts healthy canaries and fails when conditions flap false or
// unhealthy count exceeds maxUnhealthy.
func EvaluateSoak(items []HealthItem, soakStart time.Time, now time.Time, maxUnhealthy int32) (healthy, unhealthy int32, failed bool) {
	_ = now
	for _, item := range items {
		if !item.ReadyTrue || !item.SyncedTrue {
			unhealthy++
			failed = true
			continue
		}

		readyStable := !item.ReadySince.After(soakStart)
		syncedStable := !item.SyncedSince.After(soakStart)
		if readyStable && syncedStable {
			healthy++
		}
		// True but flipped after soakStart is still stabilising; not healthy yet
		// and not counted unhealthy until conditions go False.
	}

	if unhealthy > maxUnhealthy {
		failed = true
	}
	return healthy, unhealthy, failed
}
