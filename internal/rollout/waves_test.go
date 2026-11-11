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

	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestComputeBatchSizeInt(t *testing.T) {
	bs := intstr.FromInt32(5)
	if got := ComputeBatchSize(25, bs); got != 5 {
		t.Fatalf("got %d want 5", got)
	}
	if got := ComputeBatchSize(3, bs); got != 3 {
		t.Fatalf("got %d want 3", got)
	}
}

func TestComputeBatchSizePercent(t *testing.T) {
	bs := intstr.FromString("10%")
	if got := ComputeBatchSize(25, bs); got != 3 {
		t.Fatalf("got %d want 3", got)
	}
	if got := ComputeBatchSize(5, bs); got != 1 {
		t.Fatalf("got %d want 1", got)
	}
}

func TestSelectNextWaveStableOrder(t *testing.T) {
	pending := []Candidate{
		{Namespace: "b", Name: "xr-2"},
		{Namespace: "a", Name: "xr-2"},
		{Namespace: "a", Name: "xr-1"},
	}
	wave := SelectNextWave(pending, 2)
	if len(wave) != 2 {
		t.Fatalf("expected 2, got %d", len(wave))
	}
	if wave[0].Namespace != "a" || wave[0].Name != "xr-1" {
		t.Fatalf("unexpected first: %+v", wave[0])
	}
	if wave[1].Namespace != "a" || wave[1].Name != "xr-2" {
		t.Fatalf("unexpected second: %+v", wave[1])
	}
}
