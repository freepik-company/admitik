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

package clustergenerationpolicies

import (
	"context"
	"slices"
	"strings"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"freepik.com/admitik/api/v1alpha1"
)

const (

	//
	resourceUpdatedMessage  = "A ClusterGenerationPolicy was modified: will be updated into the internal registry"
	resourceDeletionMessage = "A ClusterGenerationPolicy was deleted: will be deleted from internal registry"
)

var (
	AllowedWatchOperations = []admissionregv1.OperationType{
		admissionregv1.Create,
		admissionregv1.Update,
		admissionregv1.Delete,
		admissionregv1.Connect}

	//
)

// ReconcileClusterGenerationPolicy keeps internal ClusterGenerationPolicy resources' registry up-to-date
func (r *ClusterGenerationPolicyReconciler) ReconcileClusterGenerationPolicy(ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterGenerationPolicy) (err error) {
	logger := log.FromContext(ctx)

	// Replace wildcards in operations
	if slices.Contains(resourceManifest.Spec.WatchedResources.Operations, admissionregv1.OperationAll) {
		resourceManifest.Spec.WatchedResources.Operations = AllowedWatchOperations
	}

	// Update the registry
	var desiredWatchedTypes []string
	for _, operation := range resourceManifest.Spec.WatchedResources.Operations {

		// Skip unsupported operations
		if !slices.Contains(AllowedWatchOperations, operation) {
			continue
		}

		// Create the key-pattern and store it for later cleaning
		// Duplicated operations will be skipped
		watchedType := strings.Join([]string{
			resourceManifest.Spec.WatchedResources.Group,
			resourceManifest.Spec.WatchedResources.Version,
			resourceManifest.Spec.WatchedResources.Resource,
			string(operation),
		}, "/")

		if slices.Contains(desiredWatchedTypes, watchedType) {
			continue
		}
		desiredWatchedTypes = append(desiredWatchedTypes, watchedType)

		// Handle deletion requests
		if eventType == watch.Deleted {
			logger.Info(resourceDeletionMessage, "watcher", watchedType)

			r.Dependencies.ClusterGenerationPoliciesRegistry.RemoveResource(watchedType, resourceManifest)
		}

		// Handle creation/update requests
		if eventType == watch.Modified {
			logger.Info(resourceUpdatedMessage, "watcher", watchedType)

			r.Dependencies.ClusterGenerationPoliciesRegistry.RemoveResource(watchedType, resourceManifest)
			r.Dependencies.ClusterGenerationPoliciesRegistry.AddResource(watchedType, resourceManifest)
		}
	}

	// Clean non-desired watched types. This is needed for updates where the user
	// reduces the amount of watched operations on watched resources
	for _, registeredResourceType := range r.Dependencies.ClusterGenerationPoliciesRegistry.GetRegisteredResourceTypes() {
		if !slices.Contains(desiredWatchedTypes, registeredResourceType) {
			r.Dependencies.ClusterGenerationPoliciesRegistry.RemoveResource(registeredResourceType, resourceManifest)
		}
	}

	// TODO: Discuss
	// Some ClusterGenerationPolicy changed,
	// do we want to update all watched resources to reconcile everything retroactively,
	// or just admit future events?

	return nil
}
