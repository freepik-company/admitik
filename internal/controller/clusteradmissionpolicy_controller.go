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
	"context"
	"fmt"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	//
	"freepik.com/admitik/api/v1alpha1"
)

const (
	notificationNotFoundError          = "Notification resource not found. Ignoring since object must be deleted."
	notificationRetrievalError         = "Error getting the notification from the cluster"
	notificationFinalizersUpdateError  = "Failed to update finalizer of notification: %s"
	notificationConditionUpdateError   = "Failed to update the condition on notification: %s"
	notificationSyncTimeRetrievalError = "Can not get synchronization time from the notification: %s"
	notificationReconcileError         = "Can not reconcile Notification: %s"
)

// TODO
type ClusterAdmissionPolicyControllerOptions struct {
	WebhookClientConfig admissionregv1.WebhookClientConfig
}

// ClusterAdmissionPolicyReconciler reconciles a ClusterAdmissionPolicy object
type ClusterAdmissionPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	//
	Options ClusterAdmissionPolicyControllerOptions
}

// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clusteradmissionpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clusteradmissionpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clusteradmissionpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;create;update;patch;delete;watch
// +kubebuilder:rbac:groups="*",resources="*",verbs="*"

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.2/pkg/reconcile
func (r *ClusterAdmissionPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	_ = logger

	// 1. Get the content of the object
	reqObject := &v1alpha1.ClusterAdmissionPolicy{}
	err = r.Get(ctx, req.NamespacedName, reqObject)

	// 2. Check the existence inside the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(notificationNotFoundError)
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(notificationRetrievalError)
		return result, err
	}

	// 3. Check if the object instance is marked to be deleted: indicated by the deletion timestamp being set
	if !reqObject.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(reqObject, resourceFinalizer) {

			// Delete AdmissionPolicy from AdmissionPool
			err = r.SyncAdmissionPool(ctx, watch.Deleted, reqObject)
			if err != nil {
				logger.Info(fmt.Sprintf(notificationReconcileError, reqObject.Name))
				return result, err
			}

			// Remove the finalizers on CR
			controllerutil.RemoveFinalizer(reqObject, resourceFinalizer)
			err = r.Update(ctx, reqObject)
			if err != nil {
				logger.Info(fmt.Sprintf(notificationFinalizersUpdateError, req.Name))
			}
		}
		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the CR
	if !controllerutil.ContainsFinalizer(reqObject, resourceFinalizer) {
		controllerutil.AddFinalizer(reqObject, resourceFinalizer)
		err = r.Update(ctx, reqObject)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, reqObject)
		if err != nil {
			logger.Info(fmt.Sprintf(notificationConditionUpdateError, req.Name))
		}
	}()

	// 7. The object CR already exists: manage the update
	err = r.SyncAdmissionPool(ctx, watch.Modified, reqObject)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(reqObject)
		logger.Info(fmt.Sprintf(notificationReconcileError, reqObject.Name))
		return result, err
	}

	// 8. Success, update the status
	r.UpdateConditionSuccess(reqObject)

	//logger.Info(fmt.Sprintf(scheduleSynchronization, result.RequeueAfter.String()))
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterAdmissionPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterAdmissionPolicy{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
