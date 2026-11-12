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

package promoter

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutsv1alpha1 "github.com/kanzifucius/xr-rollout-controller/api/v1alpha1"
)

// FieldManager owns the promoter's Start entry and nothing else.
const FieldManager = "xr-rollout-promoter"

// TriggerWriter appends one Start trigger to a CompositionRollout.
type TriggerWriter interface {
	AppendStart(ctx context.Context, cl client.Client, rolloutName string, trigger rolloutsv1alpha1.RolloutTrigger) error
}

type ssaWriter struct{}

// NewSSAWriter returns a writer that server-side applies one trigger map entry.
func NewSSAWriter() TriggerWriter { return ssaWriter{} }

func (ssaWriter) AppendStart(ctx context.Context, cl client.Client, rolloutName string, trigger rolloutsv1alpha1.RolloutTrigger) error {
	obj := &rolloutsv1alpha1.CompositionRollout{}
	obj.Name = rolloutName
	obj.SetGroupVersionKind(rolloutsv1alpha1.GroupVersion.WithKind("CompositionRollout"))
	obj.Spec.Triggers = []rolloutsv1alpha1.RolloutTrigger{trigger}
	if err := cl.Patch(ctx, obj, client.Apply, client.FieldOwner(FieldManager)); err != nil {
		return fmt.Errorf("apply Start on %s: %w", rolloutName, err)
	}
	return nil
}
