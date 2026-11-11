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

package xp

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	fieldSpec                        = "spec"
	fieldCrossplane                  = "crossplane"
	fieldCompositionRef              = "compositionRef"
	fieldCompositionRevisionRef      = "compositionRevisionRef"
	fieldName                        = "name"
	fieldCompositionUpdatePolicy     = "compositionUpdatePolicy"
	fieldStatus                      = "status"
	fieldConditions                  = "conditions"
	conditionTypeReady               = "Ready"
	conditionTypeSynced              = "Synced"
	compositionUpdatePolicyAutomatic = "Automatic"
	compositionUpdatePolicyManual    = "Manual"
)

// GetCompositionRefName returns spec.crossplane.compositionRef.name.
func GetCompositionRefName(u *unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(u.Object, fieldSpec, fieldCrossplane, fieldCompositionRef, fieldName)
	return name
}

// GetCompositionUpdatePolicy returns Manual or Automatic; missing defaults to Automatic.
// ManualCompositionUpdatePolicy is the Manual composition update policy value.
const ManualCompositionUpdatePolicy = compositionUpdatePolicyManual

func GetCompositionUpdatePolicy(u *unstructured.Unstructured) string {
	if pol, found, _ := unstructured.NestedString(u.Object, fieldSpec, fieldCrossplane, fieldCompositionUpdatePolicy); found && pol != "" {
		return pol
	}
	if pol, found, _ := unstructured.NestedString(u.Object, fieldSpec, fieldCompositionUpdatePolicy); found && pol != "" {
		return pol
	}
	return compositionUpdatePolicyAutomatic
}

// GetCompositionRevisionRef returns spec.crossplane.compositionRevisionRef.name.
func GetCompositionRevisionRef(u *unstructured.Unstructured) string {
	name, _, _ := unstructured.NestedString(u.Object, fieldSpec, fieldCrossplane, fieldCompositionRevisionRef, fieldName)
	return name
}

// SetCompositionRevisionRef sets only spec.crossplane.compositionRevisionRef.name.
func SetCompositionRevisionRef(u *unstructured.Unstructured, name string) {
	_ = unstructured.SetNestedField(u.Object, name, fieldSpec, fieldCrossplane, fieldCompositionRevisionRef, fieldName)
}

// IsReadyAndSynced reports whether Ready and Synced conditions are True.
func IsReadyAndSynced(u *unstructured.Unstructured) (healthy bool, readyCond *metav1.Condition, syncedCond *metav1.Condition) {
	readyCond = findCondition(u, conditionTypeReady)
	syncedCond = findCondition(u, conditionTypeSynced)
	healthy = conditionIsTrue(readyCond) && conditionIsTrue(syncedCond)
	return healthy, readyCond, syncedCond
}

// MatchesXRSelector reports whether the XR matches namespace and label constraints.
func MatchesXRSelector(u *unstructured.Unstructured, namespaces []string, labelSelector *metav1.LabelSelector) bool {
	if len(namespaces) > 0 {
		ns := u.GetNamespace()
		found := false
		for _, want := range namespaces {
			if want == ns {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if labelSelector == nil {
		return true
	}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return false
	}
	return selector.Matches(labels.Set(u.GetLabels()))
}

func findCondition(u *unstructured.Unstructured, condType string) *metav1.Condition {
	raw, found, _ := unstructured.NestedSlice(u.Object, fieldStatus, fieldConditions)
	if !found {
		return nil
	}
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(m, "type")
		if t != condType {
			continue
		}
		cond := conditionFromMap(m)
		return &cond
	}
	return nil
}

func conditionFromMap(m map[string]interface{}) metav1.Condition {
	cond := metav1.Condition{}
	if v, _, _ := unstructured.NestedString(m, "type"); v != "" {
		cond.Type = v
	}
	if v, _, _ := unstructured.NestedString(m, "status"); v != "" {
		cond.Status = metav1.ConditionStatus(v)
	}
	if v, _, _ := unstructured.NestedString(m, "reason"); v != "" {
		cond.Reason = v
	}
	if v, _, _ := unstructured.NestedString(m, "message"); v != "" {
		cond.Message = v
	}
	if v, _, _ := unstructured.NestedString(m, "lastTransitionTime"); v != "" {
		if t, err := parseTime(v); err == nil {
			cond.LastTransitionTime = t
		}
	}
	return cond
}

func parseTime(s string) (metav1.Time, error) {
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return metav1.Time{}, err
	}
	return metav1.NewTime(parsed), nil
}

func conditionIsTrue(c *metav1.Condition) bool {
	return c != nil && c.Status == metav1.ConditionTrue
}
