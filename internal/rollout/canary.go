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
	"fmt"
	"hash/fnv"
	"sort"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

// Candidate identifies one XR in the governed fleet.
type Candidate struct {
	Namespace string
	Name      string
	UID       string
}

// SelectCanaries chooses the canary subset according to strategy.
func SelectCanaries(
	candidates []Candidate,
	strategy rolloutsv1alpha1.CanaryStrategy,
	count *int32,
	percent *int32,
	preferredNamespaces []string,
	labelMatch func(Candidate) bool,
) ([]Candidate, error) {
	switch strategy {
	case rolloutsv1alpha1.CanaryStrategyLabelSelector:
		var out []Candidate
		for _, c := range candidates {
			if labelMatch != nil && labelMatch(c) {
				out = append(out, c)
			}
		}
		return out, nil
	case rolloutsv1alpha1.CanaryStrategyCount:
		if count == nil {
			return nil, fmt.Errorf("count strategy requires count")
		}
		n := int(*count)
		if n < 0 {
			return nil, fmt.Errorf("count must be non-negative")
		}
		return takeDeterministic(candidates, preferredNamespaces, n), nil
	case rolloutsv1alpha1.CanaryStrategyPercent:
		if percent == nil {
			return nil, fmt.Errorf("percent strategy requires percent")
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		n := percentCount(len(candidates), *percent)
		return takeDeterministic(candidates, preferredNamespaces, n), nil
	default:
		return nil, fmt.Errorf("unsupported canary strategy %q", strategy)
	}
}

func percentCount(fleetLen int, percent int32) int {
	if fleetLen == 0 {
		return 0
	}
	if percent <= 0 {
		return 1
	}
	// Round up: ceil(fleetLen * percent / 100), minimum 1.
	n := (fleetLen*int(percent) + 99) / 100
	if n < 1 {
		n = 1
	}
	if n > fleetLen {
		n = fleetLen
	}
	return n
}

func takeDeterministic(candidates []Candidate, preferredNamespaces []string, n int) []Candidate {
	if n <= 0 || len(candidates) == 0 {
		return nil
	}
	if n > len(candidates) {
		n = len(candidates)
	}

	ordered := orderCandidates(candidates, preferredNamespaces)
	return ordered[:n]
}

type hashedCandidate struct {
	c    Candidate
	hash uint32
}

func orderCandidates(candidates []Candidate, preferredNamespaces []string) []Candidate {
	if len(preferredNamespaces) == 0 {
		sorted := hashSort(candidates)
		out := make([]Candidate, len(sorted))
		for i, h := range sorted {
			out[i] = h.c
		}
		return out
	}

	preferredSet := make(map[string]struct{}, len(preferredNamespaces))
	for _, ns := range preferredNamespaces {
		preferredSet[ns] = struct{}{}
	}

	byNS := make(map[string][]Candidate)
	var rest []Candidate
	for _, c := range candidates {
		if _, ok := preferredSet[c.Namespace]; ok {
			byNS[c.Namespace] = append(byNS[c.Namespace], c)
		} else {
			rest = append(rest, c)
		}
	}

	var ordered []Candidate
	for _, ns := range preferredNamespaces {
		bucket := byNS[ns]
		if len(bucket) == 0 {
			continue
		}
		for _, h := range hashSort(bucket) {
			ordered = append(ordered, h.c)
		}
	}
	for _, h := range hashSort(rest) {
		ordered = append(ordered, h.c)
	}

	return ordered
}

func hashSort(candidates []Candidate) []hashedCandidate {
	hashed := make([]hashedCandidate, len(candidates))
	for i, c := range candidates {
		hashed[i] = hashedCandidate{c: c, hash: fnvHashUID(c.UID)}
	}
	sort.SliceStable(hashed, func(i, j int) bool {
		if hashed[i].hash != hashed[j].hash {
			return hashed[i].hash < hashed[j].hash
		}
		if hashed[i].c.Namespace != hashed[j].c.Namespace {
			return hashed[i].c.Namespace < hashed[j].c.Namespace
		}
		return hashed[i].c.Name < hashed[j].c.Name
	})
	return hashed
}

func fnvHashUID(uid string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(uid))
	return h.Sum32()
}
