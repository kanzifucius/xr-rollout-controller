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
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

// FieldIndexCompositionName indexes CompositionRollout by spec.compositions[].name.
const FieldIndexCompositionName = "spec.compositions"

func setupIndexes(mgr ctrl.Manager) error {
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(ctx, &rolloutsv1alpha1.CompositionRollout{}, FieldIndexCompositionName, func(raw client.Object) []string {
		cr, ok := raw.(*rolloutsv1alpha1.CompositionRollout)
		if !ok {
			return nil
		}
		names := make([]string, 0, len(cr.Spec.Compositions))
		for i := range cr.Spec.Compositions {
			names = append(names, cr.Spec.Compositions[i].Name)
		}
		return names
	}); err != nil {
		return fmt.Errorf("index CompositionRollout: %w", err)
	}
	// XR kinds are not one GVK. Indexing a typeless unstructured.Unstructured
	// makes controller-runtime reject the object ("no kind") and the manager
	// never starts. XR reads go through the dynamic client.
	return nil
}
