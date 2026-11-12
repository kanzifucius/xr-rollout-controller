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

package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestPromotionPipelineRegisters(t *testing.T) {
	t.Parallel()
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	gvk := GroupVersion.WithKind("PromotionPipeline")
	obj, err := s.New(gvk)
	if err != nil {
		t.Fatalf("New(%s): %v", gvk, err)
	}
	if _, ok := obj.(*PromotionPipeline); !ok {
		t.Fatalf("got %T, want *PromotionPipeline", obj)
	}
}
