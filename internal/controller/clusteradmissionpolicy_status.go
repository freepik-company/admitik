/*
Copyright 2024.

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
	"freepik.com/admitik/internal/globals"

	admitikv1alpha1 "freepik.com/admitik/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *ClusterAdmissionPolicyReconciler) UpdateConditionSuccess(ClusterAdmissionPolicy *admitikv1alpha1.ClusterAdmissionPolicy) {

	//
	condition := globals.NewCondition(globals.ConditionTypeResourceSynced, metav1.ConditionTrue,
		globals.ConditionReasonTargetSynced, globals.ConditionReasonTargetSyncedMessage)

	globals.UpdateCondition(&ClusterAdmissionPolicy.Status.Conditions, condition)
}

func (r *ClusterAdmissionPolicyReconciler) UpdateConditionKubernetesApiCallFailure(ClusterAdmissionPolicy *admitikv1alpha1.ClusterAdmissionPolicy) {

	//
	condition := globals.NewCondition(globals.ConditionTypeResourceSynced, metav1.ConditionTrue,
		globals.ConditionReasonKubernetesApiCallErrorType, globals.ConditionReasonKubernetesApiCallErrorMessage)

	globals.UpdateCondition(&ClusterAdmissionPolicy.Status.Conditions, condition)
}
