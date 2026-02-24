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

package informer

import (
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/watch"
)

// ResourceKey identifies an informer by the resource it watches.
// Format is either GVR ("{group}/{version}/{resource}") for sources
// or GVRNN ("{group}/{version}/{resource}/{namespace}/{name}") for watched resources.
type ResourceKey = string

// EventListener receives broadcast events from the Registry whenever a watched
// informer delivers an Add/Update/Delete from Kubernetes. Implementations decide
// internally whether they care about a given key.
type EventListener interface {
	OnEvent(resourceKey ResourceKey, eventType watch.EventType, objects ...map[string]interface{})
}

// InformerEntry holds the runtime state of a single dynamic informer managed by the Registry.
//
// Fields:
//   - Started: ACK flag — true while the informer goroutine is alive.
//   - StopSignal: buffered(1) channel used to kill the informer without deadlock.
//   - consumers: refcount map — each key is a consumer name following the pattern
//     "{prefix}:{policyKind}:{policyName}" (e.g. "sources:ClusterGenerationPolicy:gen-labels").
//     The informer stays alive as long as len(consumers) > 0.
//   - pool: cached Kubernetes objects for sources informers. Pointers to maps for
//     zero-copy performance — do not mutate the pointed-to maps.
type InformerEntry struct {
	mu sync.Mutex

	Started    bool
	StopSignal chan struct{}

	consumers map[string]bool

	pool []*map[string]interface{}
}

// Lock acquires the entry-level mutex. Used by InformerManager when iterating
// consumers to identify stale entries without holding the registry-level lock.
func (e *InformerEntry) Lock() { e.mu.Lock() }

// Unlock releases the entry-level mutex.
func (e *InformerEntry) Unlock() { e.mu.Unlock() }

// GetConsumers returns a snapshot copy of the consumers map.
// Caller must hold the entry-level mutex (via Lock/Unlock).
func (e *InformerEntry) GetConsumers() map[string]bool {
	result := make(map[string]bool, len(e.consumers))
	for k, v := range e.consumers {
		result[k] = v
	}
	return result
}

// Registry is the unified informer registry. It manages:
//   - Informer lifecycle (register/unregister/start/stop) via refcounted consumers.
//   - Object pool (sources cache) per informer key.
//   - Event broadcast to registered EventListener implementations.
//
// Thread-safe: all public methods are protected by sync.RWMutex at the registry level,
// with per-entry sync.Mutex for pool and started-flag access.
type Registry struct {
	mu        sync.RWMutex
	informers map[ResourceKey]*InformerEntry

	listenersMu sync.RWMutex
	listeners   []EventListener
}

// NewRegistry creates an empty Registry ready to accept informer registrations.
func NewRegistry() *Registry {
	return &Registry{
		informers: make(map[ResourceKey]*InformerEntry),
	}
}

// AddListener appends an EventListener that will receive all future Broadcast calls.
// Listeners are invoked concurrently in separate goroutines (fire-and-forget).
func (r *Registry) AddListener(l EventListener) {
	r.listenersMu.Lock()
	defer r.listenersMu.Unlock()
	r.listeners = append(r.listeners, l)
}

// Broadcast sends an informer event to all registered listeners.
// Each listener is invoked in its own goroutine to avoid blocking the informer.
func (r *Registry) Broadcast(key ResourceKey, eventType watch.EventType, objects ...map[string]interface{}) {
	r.listenersMu.RLock()
	defer r.listenersMu.RUnlock()
	for _, l := range r.listeners {
		go l.OnEvent(key, eventType, objects...)
	}
}

// Register adds a consumer to the informer identified by key. If the informer entry
// does not exist yet, it is created with a buffered(1) StopSignal channel.
//
// Consumer names follow the pattern "{prefix}:{policyKind}:{policyName}", e.g.:
//   - "sources:ClusterGenerationPolicy:gen-labels"
//   - "watched:ClusterCleanPolicy:clean-orphans"
func (r *Registry) Register(key ResourceKey, consumer string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.informers[key]
	if !exists {
		entry = &InformerEntry{
			StopSignal: make(chan struct{}, 1),
			consumers:  make(map[string]bool),
		}
		r.informers[key] = entry
	}
	entry.consumers[consumer] = true
}

// Unregister removes a specific consumer from the informer identified by key.
// Does nothing if the key or consumer does not exist.
// After unregistering, callers should check ConsumerCount and call Disable if zero.
func (r *Registry) Unregister(key ResourceKey, consumer string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.informers[key]
	if !exists {
		return
	}
	delete(entry.consumers, consumer)
}

// UnregisterByPrefix removes all consumers whose name starts with the given prefix
// from the informer identified by key. Useful for bulk cleanup when an entire
// policy kind or prefix scope is no longer relevant.
//
// Returns the number of consumers removed.
func (r *Registry) UnregisterByPrefix(key ResourceKey, prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.informers[key]
	if !exists {
		return 0
	}

	removed := 0
	for consumer := range entry.consumers {
		if strings.HasPrefix(consumer, prefix) {
			delete(entry.consumers, consumer)
			removed++
		}
	}
	return removed
}

// ConsumerCount returns the number of active consumers for the given informer key.
// Returns 0 if the key does not exist.
func (r *Registry) ConsumerCount(key ResourceKey) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.informers[key]
	if !exists {
		return 0
	}
	return len(entry.consumers)
}

// HasConsumer checks if a specific consumer name is registered for the given key.
func (r *Registry) HasConsumer(key ResourceKey, consumer string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.informers[key]
	if !exists {
		return false
	}
	return entry.consumers[consumer]
}

// HasAnyConsumerWithPrefix checks if any consumer registered for the given key
// has a name starting with the specified prefix. Used by WatchedEventListener
// to determine if any policy of a given kind is interested in events for this key.
func (r *Registry) HasAnyConsumerWithPrefix(key ResourceKey, prefix string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.informers[key]
	if !exists {
		return false
	}
	for consumer := range entry.consumers {
		if strings.HasPrefix(consumer, prefix) {
			return true
		}
	}
	return false
}

// GetEntry returns the InformerEntry for the given key, or (nil, false) if not found.
// The returned entry is a pointer to the live entry — callers must use its mutex
// for field access.
func (r *Registry) GetEntry(key ResourceKey) (entry *InformerEntry, exists bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, exists = r.informers[key]
	return entry, exists
}

// GetRegisteredKeys returns a snapshot of all informer keys currently in the registry.
func (r *Registry) GetRegisteredKeys() []ResourceKey {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]ResourceKey, 0, len(r.informers))
	for k := range r.informers {
		keys = append(keys, k)
	}
	return keys
}

// SetStarted updates the Started ACK flag for the informer identified by key.
// Does nothing if the key does not exist.
func (r *Registry) SetStarted(key ResourceKey, started bool) {
	r.mu.RLock()
	entry, exists := r.informers[key]
	r.mu.RUnlock()
	if !exists {
		return
	}
	entry.mu.Lock()
	entry.Started = started
	entry.mu.Unlock()
}

// IsStarted returns whether the informer identified by key has its Started flag set.
// Returns false if the key does not exist.
func (r *Registry) IsStarted(key ResourceKey) bool {
	r.mu.RLock()
	entry, exists := r.informers[key]
	r.mu.RUnlock()
	if !exists {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.Started
}

// Disable removes the informer entry from the registry and sends a non-blocking
// stop signal to kill its goroutine. The buffered(1) channel prevents deadlock
// if the informer has already exited.
func (r *Registry) Disable(key ResourceKey) {
	r.mu.Lock()
	entry, exists := r.informers[key]
	if !exists {
		r.mu.Unlock()
		return
	}
	delete(r.informers, key)
	r.mu.Unlock()

	select {
	case entry.StopSignal <- struct{}{}:
	default:
	}
}

// GetPool returns the cached objects slice for the given informer key.
// Returns nil if the key does not exist. The returned slice contains pointers
// to maps for zero-copy performance — callers must not mutate the pointed-to maps.
func (r *Registry) GetPool(key ResourceKey) []*map[string]interface{} {
	r.mu.RLock()
	entry, exists := r.informers[key]
	r.mu.RUnlock()
	if !exists {
		return nil
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.pool
}

// AddToPool appends a Kubernetes object pointer to the sources cache for the given key.
// Does nothing if the key does not exist.
func (r *Registry) AddToPool(key ResourceKey, obj *map[string]interface{}) {
	r.mu.RLock()
	entry, exists := r.informers[key]
	r.mu.RUnlock()
	if !exists {
		return
	}
	entry.mu.Lock()
	entry.pool = append(entry.pool, obj)
	entry.mu.Unlock()
}

// RemoveFromPool removes a Kubernetes object from the sources cache for the given key,
// matching by namespace and name extracted from the object's metadata.
// Does nothing if the key does not exist or no matching object is found.
func (r *Registry) RemoveFromPool(key ResourceKey, namespace, name string) {
	r.mu.RLock()
	entry, exists := r.informers[key]
	r.mu.RUnlock()
	if !exists {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()

	for i, obj := range entry.pool {
		objMeta, ok := (*obj)["metadata"].(map[string]interface{})
		if !ok {
			continue
		}
		objName, _ := objMeta["name"].(string)
		objNamespace, _ := objMeta["namespace"].(string)
		if objName == name && objNamespace == namespace {
			entry.pool = append(entry.pool[:i], entry.pool[i+1:]...)
			return
		}
	}
}
