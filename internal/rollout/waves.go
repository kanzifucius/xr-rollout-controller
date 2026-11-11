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
	"math"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
)

// ComputeBatchSize resolves int or percent batch sizing against remaining work.
func ComputeBatchSize(remaining int, batchSize intstr.IntOrString) int {
	if remaining <= 0 {
		return 0
	}

	switch batchSize.Type {
	case intstr.Int:
		n := batchSize.IntValue()
		if n <= 0 {
			return remaining
		}
		if n > remaining {
			return remaining
		}
		return n
	case intstr.String:
		raw := strings.TrimSpace(batchSize.StrVal)
		if raw == "" {
			return remaining
		}
		if strings.HasSuffix(raw, "%") {
			pctStr := strings.TrimSuffix(raw, "%")
			pct, err := strconv.Atoi(pctStr)
			if err != nil || pct <= 0 {
				return 1
			}
			n := int(math.Ceil(float64(remaining) * float64(pct) / 100.0))
			if n < 1 {
				n = 1
			}
			if n > remaining {
				n = remaining
			}
			return n
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return 1
		}
		if n > remaining {
			return remaining
		}
		return n
	default:
		return remaining
	}
}

// SelectNextWave returns the next wave sorted by namespace/name for stability.
func SelectNextWave(pending []Candidate, batchSize int) []Candidate {
	if batchSize <= 0 || len(pending) == 0 {
		return nil
	}

	sorted := append([]Candidate(nil), pending...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Namespace != sorted[j].Namespace {
			return sorted[i].Namespace < sorted[j].Namespace
		}
		return sorted[i].Name < sorted[j].Name
	})

	if batchSize > len(sorted) {
		batchSize = len(sorted)
	}
	return sorted[:batchSize]
}
