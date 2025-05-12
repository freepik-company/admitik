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
	"strings"

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

// ReconcileClusterGenerationPolicy keeps internal ClusterGenerationPolicy resources' registry up-to-date
func (r *ClusterGenerationPolicyReconciler) ReconcileClusterGenerationPolicy(ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterGenerationPolicy) (err error) {
	logger := log.FromContext(ctx)

	// Create the key-pattern and store it for later cleaning
	watchedType := strings.Join([]string{
		resourceManifest.Spec.WatchedResources.Group,
		resourceManifest.Spec.WatchedResources.Version,
		resourceManifest.Spec.WatchedResources.Resource,
		resourceManifest.Spec.WatchedResources.Namespace,
		resourceManifest.Spec.WatchedResources.Name,
	}, "/")

	// Handle deletion requests
	if eventType == watch.Deleted {
		logger.Info(resourceDeletionMessage, "watcher", watchedType)

		r.Dependencies.ClusterGenerationPoliciesRegistry.RemoveResource(watchedType, resourceManifest)
	}

	// Handle creation/update requests
	if eventType == watch.Modified {
		logger.Info(resourceUpdatedMessage, "watcher", watchedType)

		for _, registeredResourceType := range r.Dependencies.ClusterGenerationPoliciesRegistry.GetRegisteredResourceTypes() {
			r.Dependencies.ClusterGenerationPoliciesRegistry.RemoveResource(registeredResourceType, resourceManifest)
		}

		r.Dependencies.ClusterGenerationPoliciesRegistry.AddResource(watchedType, resourceManifest)
	}

	// TODO: Discuss
	// Some ClusterGenerationPolicy changed,
	// do we want to update all watched resources to reconcile everything retroactively,
	// or just admit future events?

	return nil
}
