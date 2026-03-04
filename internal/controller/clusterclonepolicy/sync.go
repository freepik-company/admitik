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

package clusterclonepolicy

import (
	"context"
	"slices"

	//
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/keys"
)

const (

	//
	resourceUpdatedMessage  = "A ClusterClonePolicy was modified: will be updated into the internal registry"
	resourceDeletionMessage = "A ClusterClonePolicy was deleted: will be deleted from internal registry"
)

// ReconcileClusterClonePolicy keeps internal ClusterClonePolicy resources' registry up-to-date
func (r *ClusterClonePolicyReconciler) ReconcileClusterClonePolicy(ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterClonePolicy) (err error) {
	logger := log.FromContext(ctx)

	desiredWatchedGroups := []string{}
	// Update the registry
	for _, watchedResourceGroup := range resourceManifest.Spec.WatchedResources {

		// Create the key-pattern and store it for later cleaning
		watchedType := keys.GVRNNKey(
			watchedResourceGroup.Group,
			watchedResourceGroup.Version,
			watchedResourceGroup.Resource,
			watchedResourceGroup.Namespace,
			watchedResourceGroup.Name,
		)

		// Handle deletion requests
		if eventType == watch.Deleted {
			logger.Info(resourceDeletionMessage, "watcher", watchedType)
			r.Dependencies.ClusterClonePolicyRegistry.RemoveResource(watchedType, resourceManifest)
			continue
		}

		// Handle creation/update requests
		if eventType == watch.Modified {
			logger.Info(resourceUpdatedMessage, "watcher", watchedType)

			// Avoid adding those already added.
			// This prevents user from defining the group more than once per manifest
			if slices.Contains(desiredWatchedGroups, watchedType) {
				continue
			}
			desiredWatchedGroups = append(desiredWatchedGroups, watchedType)
			r.Dependencies.ClusterClonePolicyRegistry.AddOrUpdateResource(watchedType, resourceManifest)
		}
	}

	// Clean non-desired watched types. This is needed for updates where the user
	// changes watched resources
	for _, registeredResourceType := range r.Dependencies.ClusterClonePolicyRegistry.GetCollectionNames() {
		if !slices.Contains(desiredWatchedGroups, registeredResourceType) {
			r.Dependencies.ClusterClonePolicyRegistry.RemoveResource(registeredResourceType, resourceManifest)
			continue
		}
	}

	return nil
}
