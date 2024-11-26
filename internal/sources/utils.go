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
	"errors"
	"fmt"
	"sync"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// prepareWatcher scaffolds a new watcher in the WatchedPool.
// This prepares the field for later watchers' reconciliation process.
// That process will create the real Kubernetes informer for this object
// This function is not responsible for blocking the pool before being executed
func (r *SourcesController) prepareWatcher(watcherType resourceTypeName) {
	started := false
	blocked := false
	stopSignal := make(chan bool)
	mutex := &sync.RWMutex{}

	r.watcherPool.Pool[watcherType] = &resourceTypeWatcherT{
		Mutex:        mutex,
		Started:      &started,
		Blocked:      &blocked,
		StopSignal:   &stopSignal,
		ResourceList: make([]*unstructured.Unstructured, 0),
	}
}

// disableWatcher disables a watcher from the WatcherPool.
// It first blocks the watcher to prevent it from being started by any controller,
// then, the watcher is stopped and resources are deleted.
// This function is not responsible for blocking the pool before being executed
func (r *SourcesController) disableWatcher(watcherType resourceTypeName) (result bool) {

	// 1. Prevent watcher from being started again
	*r.watcherPool.Pool[watcherType].Blocked = true

	// 2. Stop the watcher
	*r.watcherPool.Pool[watcherType].StopSignal <- true

	// 3. Wait for the watcher to be stopped. Return false on failure
	stoppedWatcher := false
	for i := 0; i < 10; i++ {
		if !*r.watcherPool.Pool[watcherType].Started {
			stoppedWatcher = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !stoppedWatcher {
		return false
	}

	r.watcherPool.Pool[watcherType].ResourceList = []*unstructured.Unstructured{}
	return true
}

// SyncWatchers ensures the WatcherPool matches the desired state.
//
// Given a list of desired watchers in GVRNN format (Group/Version/Resource/Namespace/Name),
// this function creates missing watchers, ensures active ones are unblocked, and removes
// any watchers that are no longer needed.
func (r *SourcesController) SyncWatchers(watcherTypeList []string) (err error) {

	// 0. Check if WatcherPool is ready to work
	if r.watcherPool.Mutex == nil {
		return fmt.Errorf("watcher pool is not ready")
	}

	// 1. Small conversions to gain performance on huge watchers lists
	desiredWatchers := make(map[resourceTypeName]struct{}, len(watcherTypeList))
	for _, watcherType := range watcherTypeList {
		desiredWatchers[resourceTypeName(watcherType)] = struct{}{}
	}

	// 2. Keep or create desired watchers
	for watcherType := range desiredWatchers {

		// Lock the WatcherPool mutex for reading
		r.watcherPool.Mutex.RLock()
		watcher, exists := r.watcherPool.Pool[watcherType]
		r.watcherPool.Mutex.RUnlock()

		if !exists {
			// Lock the watcher's mutex for writing
			r.watcherPool.Mutex.Lock()
			r.prepareWatcher(watcherType)
			r.watcherPool.Mutex.Unlock()
			continue
		}

		// Ensure the watcher is NOT blocked
		watcher.Mutex.Lock()
		if !*watcher.Started {
			*watcher.Blocked = false
		}
		watcher.Mutex.Unlock()
	}

	// 3. Clean undesired watchers
	r.watcherPool.Mutex.RLock()
	existingWatchers := make([]resourceTypeName, 0, len(r.watcherPool.Pool))
	for watcherType := range r.watcherPool.Pool {
		existingWatchers = append(existingWatchers, watcherType)
	}
	r.watcherPool.Mutex.RUnlock()

	for _, watcherType := range existingWatchers {
		if _, needed := desiredWatchers[watcherType]; !needed {
			// Lock WatcherPool to access the watcher
			r.watcherPool.Mutex.RLock()
			watcher := r.watcherPool.Pool[watcherType]
			r.watcherPool.Mutex.RUnlock()

			watcher.Mutex.Lock()
			watcherDisabled := r.disableWatcher(watcherType)
			watcher.Mutex.Unlock()

			if !watcherDisabled {
				err = errors.Join(err, fmt.Errorf("imposible to disable watcher for: %s", watcherType))
			}

			// Delete the watcher from the WatcherPool
			r.watcherPool.Mutex.Lock()
			delete(r.watcherPool.Pool, watcherType)
			r.watcherPool.Mutex.Unlock()
		}
	}

	return err
}

// GetWatcherResources accept a desired watcher in the GVRNN format (Group/Version/Resource/Namespace/Name)
// and returns a list of resources matching it
func (r *SourcesController) GetWatcherResources(watcherType string) (resources []*unstructured.Unstructured, err error) {

	// 0. Check if WatcherPool is ready to work
	if r.watcherPool.Mutex == nil {
		return resources, fmt.Errorf("watcher pool is not ready")
	}

	// Lock the WatcherPool mutex for reading
	r.watcherPool.Mutex.RLock()
	watcher, watcherTypeFound := r.watcherPool.Pool[resourceTypeName(watcherType)]
	r.watcherPool.Mutex.RUnlock()

	if !watcherTypeFound {
		return nil, fmt.Errorf("watcher type '%s' not found. Is the watcher created?", watcherType)
	}

	// Lock the watcher's mutex for reading
	watcher.Mutex.RLock()
	defer watcher.Mutex.RUnlock()

	// Return the pointer to the ResourceList
	return watcher.ResourceList, nil
}

// createWatcherResource TODO
func (r *SourcesController) createWatcherResource(watcherType resourceTypeName, resource *unstructured.Unstructured) error {
	// Lock the WatcherPool mutex for reading
	r.watcherPool.Mutex.RLock()
	watcher, exists := r.watcherPool.Pool[watcherType]
	r.watcherPool.Mutex.RUnlock()

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

// TODO
func (r *SourcesController) getWatcherResourceIndex(watcherType resourceTypeName, resource *unstructured.Unstructured) int {
	// Lock the WatcherPool mutex for reading
	r.watcherPool.Mutex.RLock()
	watcher, exists := r.watcherPool.Pool[watcherType]
	r.watcherPool.Mutex.RUnlock()

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

// TODO
func (r *SourcesController) updateWatcherResourceByIndex(watcherType resourceTypeName, resourceIndex int, resource *unstructured.Unstructured) error {
	// Lock the WatcherPool mutex for reading
	r.watcherPool.Mutex.RLock()
	watcher, exists := r.watcherPool.Pool[watcherType]
	r.watcherPool.Mutex.RUnlock()

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

// TODO
func (r *SourcesController) deleteWatcherResourceByIndex(watcherType resourceTypeName, resourceIndex int) error {
	// Lock the WatcherPool mutex for reading
	r.watcherPool.Mutex.RLock()
	watcher, exists := r.watcherPool.Pool[watcherType]
	r.watcherPool.Mutex.RUnlock()

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
