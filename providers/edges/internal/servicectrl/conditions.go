/*
Copyright 2026 The Faros Authors.

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

package servicectrl

import (
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	edgesv1alpha1 "github.com/faroshq/provider-edges/apis/v1alpha1"
)

// setCondition upserts a status condition, bumping LastTransitionTime only when
// the status value changes (via meta.SetStatusCondition).
func setCondition(conditions *[]metav1.Condition, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// equalStatus reports whether two ServiceStatus values are equal for the
// purpose of skipping a no-op status update. Conditions' LastTransitionTime is
// stable across no-op setCondition calls, so a plain deep-equal is sufficient.
func equalStatus(a, b *edgesv1alpha1.ServiceStatus) bool {
	return reflect.DeepEqual(a, b)
}
