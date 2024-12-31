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

package clusteradmissionpolicy

import (
	"context"
	"fmt"
	"freepik.com/admitik/internal/controller/sources"
	"sync"

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
	"freepik.com/admitik/internal/controller"
)

// TODO
type ClusterAdmissionPolicyControllerDependencies struct {
	Sources *sources.SourcesController
}

// TODO
type ClusterAdmissionPolicyControllerOptions struct {
	WebhookClientConfig admissionregv1.WebhookClientConfig
	WebhookTimeout      int
}

// ClusterAdmissionPolicyController
type ClusterAdmissionPolicyController struct {
	client.Client
	Scheme *runtime.Scheme

	// options to modify the behavior of this controller
	options ClusterAdmissionPolicyControllerOptions

	// Injected dependencies
	dependencies ClusterAdmissionPolicyControllerDependencies

	// Carried stuff
	policyPool ClusterAdmissionPolicyPoolT
}

// TODO
func NewClusterAdmissionPolicyController(options ClusterAdmissionPolicyControllerOptions,
	dependencies ClusterAdmissionPolicyControllerDependencies) *ClusterAdmissionPolicyController {

	return &ClusterAdmissionPolicyController{
		options:      options,
		dependencies: dependencies,
		policyPool: ClusterAdmissionPolicyPoolT{
			Mutex: &sync.Mutex{},
			Pool:  map[string][]v1alpha1.ClusterAdmissionPolicy{},
		},
	}
}

// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clusteradmissionpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clusteradmissionpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clusteradmissionpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;create;update;patch;delete;watch
// +kubebuilder:rbac:groups="*",resources="*",verbs="*"

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.2/pkg/reconcile
func (r *ClusterAdmissionPolicyController) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	_ = logger

	// 1. Get the content of the object
	reqObject := &v1alpha1.ClusterAdmissionPolicy{}
	err = r.Get(ctx, req.NamespacedName, reqObject)

	// 2. Check the existence inside the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.ClusterAdmissionPolicyResourceType, req.Name))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceRetrievalError, controller.ClusterAdmissionPolicyResourceType, req.Name, err.Error()))
		return result, err
	}

	// 3. Check if the object instance is marked to be deleted: indicated by the deletion timestamp being set
	if !reqObject.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(reqObject, controller.ResourceFinalizer) {

			// Delete AdmissionPolicy from AdmissionPool
			err = r.SyncAdmissionPool(ctx, watch.Deleted, reqObject)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceReconcileError, controller.ClusterAdmissionPolicyResourceType, req.Name, err.Error()))
				return result, err
			}

			// Remove the finalizers on CR
			controllerutil.RemoveFinalizer(reqObject, controller.ResourceFinalizer)
			err = r.Update(ctx, reqObject)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.ClusterAdmissionPolicyResourceType, req.Name, err.Error()))
			}
		}
		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the CR
	if !controllerutil.ContainsFinalizer(reqObject, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(reqObject, controller.ResourceFinalizer)
		err = r.Update(ctx, reqObject)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, reqObject)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.ClusterAdmissionPolicyResourceType, req.Name, err.Error()))
		}
	}()

	// 7. The object CR already exists: manage the update
	err = r.SyncAdmissionPool(ctx, watch.Modified, reqObject)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(reqObject)
		logger.Info(fmt.Sprintf(controller.ResourceReconcileError, controller.ClusterAdmissionPolicyResourceType, req.Name, err.Error()))
		return result, err
	}

	// 8. Success, update the status
	r.UpdateConditionSuccess(reqObject)

	//logger.Info(fmt.Sprintf(scheduleSynchronization, result.RequeueAfter.String()))
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterAdmissionPolicyController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterAdmissionPolicy{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// GetPolicyResources retrieves ClusterAdmissionPolicy objects matching the pattern.
// Pattern is a string formed as: /{group}/{version}/{resource}/{operation}
func (r *ClusterAdmissionPolicyController) GetPolicyResources(resourcePattern string) (resources []v1alpha1.ClusterAdmissionPolicy, err error) {

	// 0. Check if WatcherPool is ready to work
	if r.policyPool.Mutex == nil {
		return resources, fmt.Errorf("policy pool is not ready")
	}

	// Lock the PolicyPool mutex for reading
	r.policyPool.Mutex.Lock()
	policyList, policyTypeFound := r.policyPool.Pool[resourcePattern]
	r.policyPool.Mutex.Unlock()

	if !policyTypeFound {
		return nil, fmt.Errorf("no policies found matching pattern '%s'. Is the pattern right?", resourcePattern)
	}

	// Return the pointer to the resources
	return policyList, nil
}
