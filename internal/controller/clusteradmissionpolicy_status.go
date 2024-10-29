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
