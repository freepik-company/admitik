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
	"context"
	"fmt"

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
	clusterGenerationPolicyRegistry "freepik.com/admitik/internal/registry/clustergenerationpolicy"
)

type ClusterGenerationPolicyControllerOptions struct {
}

type ClusterGenerationPolicyControllerDependencies struct {
	ClusterGenerationPolicyRegistry *clusterGenerationPolicyRegistry.ClusterGenerationPolicyRegistry
}

// ClusterGenerationPolicyReconciler reconciles a ClusterGenerationPolicy object
type ClusterGenerationPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	//
	Options      ClusterGenerationPolicyControllerOptions
	Dependencies ClusterGenerationPolicyControllerDependencies
}

// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clustergenerationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clustergenerationpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=admitik.freepik.com,resources=clustergenerationpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="*",resources="*",verbs="*"

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *ClusterGenerationPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)

	// 1. Get the content of the resource
	objectManifest := &v1alpha1.ClusterGenerationPolicy{}
	err = r.Get(ctx, req.NamespacedName, objectManifest)

	// 2. Check the existence inside the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.ClusterGenerationPolicyResourceType, req.Name))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceRetrievalError, controller.ClusterGenerationPolicyResourceType, req.Name, err.Error()))
		return result, err
	}

	// 3. Check if the resource instance is marked to be deleted: indicated by the deletion timestamp being set
	if !objectManifest.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(objectManifest, controller.ResourceFinalizer) {
			// Delete Notification from WatcherPool
			err = r.ReconcileClusterGenerationPolicy(ctx, watch.Deleted, objectManifest)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceReconcileError, controller.ClusterGenerationPolicyResourceType, req.Name, err.Error()))
				return result, err
			}

			// Remove the finalizers on the resource
			controllerutil.RemoveFinalizer(objectManifest, controller.ResourceFinalizer)
			err = r.Update(ctx, objectManifest)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.ClusterGenerationPolicyResourceType, req.Name, err.Error()))
			}
		}
		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the resource
	if !controllerutil.ContainsFinalizer(objectManifest, controller.ResourceFinalizer) {
		controllerutil.AddFinalizer(objectManifest, controller.ResourceFinalizer)
		err = r.Update(ctx, objectManifest)
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = r.Status().Update(ctx, objectManifest)
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.ClusterGenerationPolicyResourceType, req.Name, err.Error()))
		}
	}()

	// 6. The resource already exists: manage the update
	err = r.ReconcileClusterGenerationPolicy(ctx, watch.Modified, objectManifest)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(objectManifest)
		logger.Info(fmt.Sprintf(controller.ResourceReconcileError, controller.ClusterGenerationPolicyResourceType, req.Name, err.Error()))
		return result, err
	}

	// 7. Success, update the status
	r.UpdateConditionSuccess(objectManifest)

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterGenerationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterGenerationPolicy{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
