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

package sources

import (
	"fmt"
	"sync"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// prepareWatcher scaffolds a new watcher in the WatchedPool
// This prepares the field for later watchers' reconciliation process.
// That process will create the real Kubernetes informer for this object
// This function is not responsible for blocking the pool before being executed
func (p *WatcherPoolT) prepareWatcher(watcherType resourceTypeName) {
	started := false
	blocked := false
	stopSignal := make(chan bool)
	mutex := &sync.RWMutex{}

	p.Pool[watcherType] = &resourceTypeWatcherT{
		Mutex:        mutex,
		Started:      &started,
		Blocked:      &blocked,
		StopSignal:   &stopSignal,
		Requesters:   make([]string, 0),
		ResourceList: make([]*unstructured.Unstructured, 0),
	}
}

// disableWatcher disables a watcher from the WatcherPool.
// It first blocks the watcher to prevent it from being started by any controller,
// then, the watcher is stopped and resources are deleted.
// This function is not responsible for blocking the pool before being executed
func (p *WatcherPoolT) disableWatcher(watcherType resourceTypeName) (result bool) {

	// 1. Prevent watcher from being started again
	*p.Pool[watcherType].Blocked = true

	// 2. Stop the watcher
	*p.Pool[watcherType].StopSignal <- true

	// 3. Wait for the watcher to be stopped. Return false on failure
	stoppedWatcher := false
	for i := 0; i < 10; i++ {
		if !*p.Pool[watcherType].Started {
			stoppedWatcher = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !stoppedWatcher {
		return false
	}

	p.Pool[watcherType].ResourceList = []*unstructured.Unstructured{}
	return true
}

// createWatcherResource adds a new resource to a watcher's resource list.
// Thread-safe using read lock for pool access and write lock for resource list.
// Returns error if watcher type doesn't exist.
func (p *WatcherPoolT) createWatcherResource(watcherType resourceTypeName, resource *unstructured.Unstructured) error {
	// Lock the WatcherPool mutex for reading
	p.Mutex.RLock()
	watcher, exists := p.Pool[watcherType]
	p.Mutex.RUnlock()

	if !exists {
		return fmt.Errorf("watcher type '%s' not found. Is the watcher created?", watcherType)
	}

	// Lock the watcher's mutex for writing
	watcher.Mutex.Lock()
	defer watcher.Mutex.Unlock()

	temporaryManifest := resource.DeepCopy()
	watcher.ResourceList = append(watcher.ResourceList, temporaryManifest)

	return nil
}

// getWatcherResourceIndex finds the index of a resource in a watcher's resource list.
// Thread-safe using read locks for pool and resource list access.
// Returns -1 if resource or watcher not found.
func (p *WatcherPoolT) getWatcherResourceIndex(watcherType resourceTypeName, resource *unstructured.Unstructured) int {
	// Lock the WatcherPool mutex for reading
	p.Mutex.RLock()
	watcher, exists := p.Pool[watcherType]
	p.Mutex.RUnlock()

	if !exists {
		return -1
	}

	// Lock the watcher's mutex for reading
	watcher.Mutex.RLock()
	defer watcher.Mutex.RUnlock()

	for index, tmpResource := range watcher.ResourceList {
		if tmpResource.GetName() == resource.GetName() &&
			tmpResource.GetNamespace() == resource.GetNamespace() {
			return index
		}
	}

	return -1
}

// updateWatcherResourceByIndex updates a resource at the specified index.
// Thread-safe using read lock for pool access and write lock for resource list.
// Returns error if watcher not found or index out of bounds.
func (p *WatcherPoolT) updateWatcherResourceByIndex(watcherType resourceTypeName, resourceIndex int, resource *unstructured.Unstructured) error {
	// Lock the WatcherPool mutex for reading
	p.Mutex.RLock()
	watcher, exists := p.Pool[watcherType]
	p.Mutex.RUnlock()

	if !exists {
		return fmt.Errorf("watcher type '%s' not found", watcherType)
	}

	// Lock the watcher's mutex for writing
	watcher.Mutex.Lock()
	defer watcher.Mutex.Unlock()

	if resourceIndex < 0 || resourceIndex >= len((*watcher).ResourceList) {
		return fmt.Errorf("resource index out of bounds")
	}

	((*watcher).ResourceList)[resourceIndex] = resource

	return nil
}

// deleteWatcherResourceByIndex removes a resource at the specified index.
// Thread-safe using read lock for pool access and write lock for resource list.
// Returns error if watcher not found or index out of bounds.
func (p *WatcherPoolT) deleteWatcherResourceByIndex(watcherType resourceTypeName, resourceIndex int) error {
	// Lock the WatcherPool mutex for reading
	p.Mutex.RLock()
	watcher, exists := p.Pool[watcherType]
	p.Mutex.RUnlock()

	if !exists {
		return fmt.Errorf("watcher type '%s' not found", watcherType)
	}

	// Lock the watcher's mutex for writing
	watcher.Mutex.Lock()
	defer watcher.Mutex.Unlock()

	if resourceIndex < 0 || resourceIndex >= len((*watcher).ResourceList) {
		return fmt.Errorf("resource index out of bounds")
	}

	// Substitute the selected notification object with the last one from the list,
	// then replace the whole list with it, minus the last.
	(*watcher).ResourceList = append(((*watcher).ResourceList)[:resourceIndex], ((*watcher).ResourceList)[resourceIndex+1:]...)

	return nil
}
