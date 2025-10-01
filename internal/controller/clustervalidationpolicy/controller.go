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

package clustervalidationpolicy

import (
	"context"
	"fmt"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerRuntimeController "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/controller"
	clusterValidationPolicyRegistry "github.com/freepik-company/admitik/internal/registry/clustervalidationpolicy"
)

type ClusterValidationPolicyControllerOptions struct {
	CurrentNamespace              string
	EnableSpecialLabels           bool
	ExcludeAdmissionSelfNamespace bool
	ExcludedAdmissionNamespaces   string

	WebhookClientConfig admissionregv1.WebhookClientConfig
	WebhookTimeout      int
}

type ClusterValidationPolicyControllerDependencies struct {
	ClusterValidationPolicyRegistry *clusterValidationPolicyRegistry.ClusterValidationPolicyRegistry
}

// ClusterValidationPolicyReconciler reconciles a ClusterValidationPolicy object
type ClusterValidationPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	//
	Options      ClusterValidationPolicyControllerOptions
	Dependencies ClusterValidationPolicyControllerDependencies
}

// +kubebuilder:rbac:groups=admitik.dev,resources=clustervalidationpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admitik.dev,resources=clustervalidationpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=admitik.dev,resources=clustervalidationpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;create;update;patch;delete;watch
// +kubebuilder:rbac:groups="*",resources="*",verbs="*"

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *ClusterValidationPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)

	// 1. Get the content of the resource
	objectManifest := &v1alpha1.ClusterValidationPolicy{}
	err = r.Get(ctx, req.NamespacedName, objectManifest)

	// 2. Check the existence inside the cluster
	if err != nil {

		// 2.1 It does NOT exist: manage removal
		if err = client.IgnoreNotFound(err); err == nil {
			logger.Info(fmt.Sprintf(controller.ResourceNotFoundError, controller.ClusterValidationPolicyResourceType, req.Name))
			return result, err
		}

		// 2.2 Failed to get the resource, requeue the request
		logger.Info(fmt.Sprintf(controller.ResourceRetrievalError, controller.ClusterValidationPolicyResourceType, req.Name, err.Error()))
		return result, err
	}

	// 3. Check if the resource instance is marked to be deleted: indicated by the deletion timestamp being set
	if !objectManifest.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(objectManifest, controller.ResourceFinalizer) {
			// Delete Notification from WatcherPool
			err = r.ReconcileClusterValidationPolicy(ctx, watch.Deleted, objectManifest)
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceReconcileError, controller.ClusterValidationPolicyResourceType, req.Name, err.Error()))
				return result, err
			}

			// Remove the finalizers on the resource
			err = controller.UpdateWithRetry(ctx, r.Client, objectManifest, func(object client.Object) error {
				controllerutil.RemoveFinalizer(object, controller.ResourceFinalizer)
				return nil
			})
			if err != nil {
				logger.Info(fmt.Sprintf(controller.ResourceFinalizersUpdateError, controller.ClusterValidationPolicyResourceType, req.Name, err.Error()))
			}
		}
		result = ctrl.Result{}
		err = nil
		return result, err
	}

	// 4. Add finalizer to the resource
	if !controllerutil.ContainsFinalizer(objectManifest, controller.ResourceFinalizer) {
		err = controller.UpdateWithRetry(ctx, r.Client, objectManifest, func(object client.Object) error {
			controllerutil.AddFinalizer(objectManifest, controller.ResourceFinalizer)
			return nil
		})
		if err != nil {
			return result, err
		}
	}

	// 5. Update the status before the requeue
	defer func() {
		err = controller.UpdateWithRetry(ctx, r.Client, objectManifest, func(object client.Object) error {
			return nil
		})
		if err != nil {
			logger.Info(fmt.Sprintf(controller.ResourceConditionUpdateError, controller.ClusterValidationPolicyResourceType, req.Name, err.Error()))
		}
	}()

	// 6. The resource already exists: manage the update
	err = r.ReconcileClusterValidationPolicy(ctx, watch.Modified, objectManifest)
	if err != nil {
		r.UpdateConditionKubernetesApiCallFailure(objectManifest)
		logger.Info(fmt.Sprintf(controller.ResourceReconcileError, controller.ClusterValidationPolicyResourceType, req.Name, err.Error()))
		return result, err
	}

	// 7. Success, update the status
	r.UpdateConditionSuccess(objectManifest)

	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterValidationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterValidationPolicy{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		WithOptions(controllerRuntimeController.Options{
			NeedLeaderElection: pointer.Bool(false),
		}).
		Complete(r)
}
