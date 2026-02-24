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

package eventprocessors

import (
	"k8s.io/apimachinery/pkg/watch"

	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
)

// PoolUpdater is an EventListener that maintains the per-key sources object pool
// in the Registry. It reacts to informer events by adding, removing, or replacing
// cached Kubernetes objects.
//
// Event handling:
//   - Added:    appends the object pointer to the pool.
//   - Modified: removes the old version by namespace+name, then appends the new one.
//   - Deleted:  removes the object by namespace+name.
//
// The pool stores []*map[string]any pointers intentionally for zero-copy performance.
// Consumers (FetchPolicySources, admission handlers) read from the pool via
// Registry.GetPool() without copying individual objects.
type PoolUpdater struct {
	registry *informerRegistry.Registry
}

// NewPoolUpdater creates a PoolUpdater bound to the given Registry.
// Must be registered via Registry.AddListener() to start receiving events.
func NewPoolUpdater(registry *informerRegistry.Registry) *PoolUpdater {
	return &PoolUpdater{registry: registry}
}

// OnEvent implements EventListener. It updates the sources object pool for the
// given resource key based on the event type.
func (p *PoolUpdater) OnEvent(resourceKey informerRegistry.ResourceKey, eventType watch.EventType, objects ...map[string]interface{}) {
	if eventType != watch.Added && eventType != watch.Modified && eventType != watch.Deleted {
		return
	}

	if len(objects) == 0 {
		return
	}

	if eventType == watch.Deleted {
		ns, name := extractNamespaceName(objects[0])
		p.registry.RemoveFromPool(resourceKey, ns, name)
		return
	}

	if eventType == watch.Modified {
		ns, name := extractNamespaceName(objects[0])
		p.registry.RemoveFromPool(resourceKey, ns, name)
	}

	p.registry.AddToPool(resourceKey, &objects[0])
}

// extractNamespaceName reads namespace and name from an unstructured object's metadata.
// Returns empty strings if metadata is missing or malformed.
func extractNamespaceName(obj map[string]interface{}) (namespace, name string) {
	objMeta, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	name, _ = objMeta["name"].(string)
	namespace, _ = objMeta["namespace"].(string)
	return namespace, name
}
