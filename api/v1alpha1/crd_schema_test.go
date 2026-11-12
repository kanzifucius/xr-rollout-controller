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
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

func TestGeneratedCRDsPassAPIServerValidation(t *testing.T) {
	t.Parallel()

	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	base := filepath.Join(filepath.Dir(file), "..", "..", "config", "crd", "bases")
	names := []string{
		"rollouts.platform.example.org_compositionrollouts.yaml",
		"rollouts.platform.example.org_promotionpipelines.yaml",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			scheme := runtime.NewScheme()
			if err := apiextensionsv1.AddToScheme(scheme); err != nil {
				t.Fatalf("add v1 scheme: %v", err)
			}
			if err := apiextensions.AddToScheme(scheme); err != nil {
				t.Fatalf("add internal scheme: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(base, name))
			if err != nil {
				t.Fatalf("read CRD: %v", err)
			}
			var in apiextensionsv1.CustomResourceDefinition
			if err := yaml.Unmarshal(raw, &in); err != nil {
				t.Fatalf("unmarshal CRD: %v", err)
			}
			out := &apiextensions.CustomResourceDefinition{}
			if err := scheme.Convert(&in, out, nil); err != nil {
				t.Fatalf("convert CRD: %v", err)
			}
			if len(out.Status.StoredVersions) == 0 {
				out.Status.StoredVersions = []string{"v1alpha1"}
			}
			errs := validation.ValidateCustomResourceDefinition(context.Background(), out)
			if len(errs) > 0 {
				t.Fatalf("CRD rejected: %v", errs.ToAggregate())
			}
		})
	}
}
