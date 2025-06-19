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

package clustergenerationpolicy

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/controller"
)

func (r *ClusterGenerationPolicyReconciler) UpdateConditionSuccess(cPolicy *v1alpha1.ClusterGenerationPolicy) {

	//
	condition := controller.NewCondition(controller.ConditionTypeResourceSynced, metav1.ConditionTrue,
		controller.ConditionReasonTargetSynced, controller.ConditionReasonTargetSyncedMessage)

	controller.UpdateCondition(&cPolicy.Status.Conditions, condition)
}

func (r *ClusterGenerationPolicyReconciler) UpdateConditionKubernetesApiCallFailure(cPolicy *v1alpha1.ClusterGenerationPolicy) {

	//
	condition := controller.NewCondition(controller.ConditionTypeResourceSynced, metav1.ConditionTrue,
		controller.ConditionReasonKubernetesApiCallErrorType, controller.ConditionReasonKubernetesApiCallErrorMessage)

	controller.UpdateCondition(&cPolicy.Status.Conditions, condition)
}
