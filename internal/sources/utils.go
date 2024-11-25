package sources

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	//
)

// prepareWatcher scaffolds a new watcher in the WatchedPool.
// This prepares the field for later watchers' reconciliation process.
// That process will create the real Kubernetes informer for this object
// This function is not responsible for blocking the pool before being executed
func (r *SourcesController) prepareWatcher(watcherType ResourceTypeName) {
	started := false
	blocked := false
	stopSignal := make(chan bool)
	mutex := &sync.RWMutex{}

	r.WatcherPool.Pool[watcherType] = &ResourceTypeWatcherT{
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
func (r *SourcesController) disableWatcher(watcherType ResourceTypeName) (result bool) {

	// 1. Prevent watcher from being started again
	*r.WatcherPool.Pool[watcherType].Blocked = true

	// 2. Stop the watcher
	*r.WatcherPool.Pool[watcherType].StopSignal <- true

	// 3. Wait for the watcher to be stopped. Return false on failure
	stoppedWatcher := false
	for i := 0; i < 10; i++ {
		if !*r.WatcherPool.Pool[watcherType].Started {
			stoppedWatcher = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !stoppedWatcher {
		return false
	}

	r.WatcherPool.Pool[watcherType].ResourceList = []*unstructured.Unstructured{}
	return true
}

// SyncWatchers ensures the WatcherPool matches the desired state.
//
// Given a list of desired watchers in GVRNN format (Group/Version/Resource/Namespace/Name),
// this function creates missing watchers, ensures active ones are unblocked, and removes
// any watchers that are no longer needed.
func (r *SourcesController) SyncWatchers(watcherTypeList []ResourceTypeName) (err error) {

	// 0. Check if WatcherPool is ready to work
	if r.WatcherPool.Mutex == nil {
		return fmt.Errorf("watcher pool is not ready")
	}

	// 1. Small conversions to gain performance on huge watchers lists
	desiredWatchers := make(map[ResourceTypeName]struct{}, len(watcherTypeList))
	for _, watcherType := range watcherTypeList {
		desiredWatchers[watcherType] = struct{}{}
	}

	// 2. Keep or create desired watchers
	for watcherType := range desiredWatchers {

		// Lock the WatcherPool mutex for reading
		r.WatcherPool.Mutex.RLock()
		watcher, exists := r.WatcherPool.Pool[watcherType]
		r.WatcherPool.Mutex.RUnlock()

		if !exists {
			// Lock the watcher's mutex for writing
			r.WatcherPool.Mutex.Lock()
			r.prepareWatcher(watcherType)
			r.WatcherPool.Mutex.Unlock()
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
	r.WatcherPool.Mutex.RLock()
	existingWatchers := make([]ResourceTypeName, 0, len(r.WatcherPool.Pool))
	for watcherType := range r.WatcherPool.Pool {
		existingWatchers = append(existingWatchers, watcherType)
	}
	r.WatcherPool.Mutex.RUnlock()

	for _, watcherType := range existingWatchers {
		if _, needed := desiredWatchers[watcherType]; !needed {
			// Lock WatcherPool to access the watcher
			r.WatcherPool.Mutex.RLock()
			watcher := r.WatcherPool.Pool[watcherType]
			r.WatcherPool.Mutex.RUnlock()

			watcher.Mutex.Lock()
			watcherDisabled := r.disableWatcher(watcherType)
			watcher.Mutex.Unlock()

			if !watcherDisabled {
				err = errors.Join(err, fmt.Errorf("imposible to disable watcher for: %s", watcherType))
			}

			// Delete the watcher from the WatcherPool
			r.WatcherPool.Mutex.Lock()
			delete(r.WatcherPool.Pool, watcherType)
			r.WatcherPool.Mutex.Unlock()
		}
	}

	return err
}

// createWatcherResource TODO
func (r *SourcesController) createWatcherResource(watcherType ResourceTypeName, resource *unstructured.Unstructured) error {
	// Lock the WatcherPool mutex for reading
	r.WatcherPool.Mutex.RLock()
	watcher, exists := r.WatcherPool.Pool[watcherType]
	r.WatcherPool.Mutex.RUnlock()

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
func (r *SourcesController) getWatcherResourceIndex(watcherType ResourceTypeName, resource *unstructured.Unstructured) int {
	// Lock the WatcherPool mutex for reading
	r.WatcherPool.Mutex.RLock()
	watcher, exists := r.WatcherPool.Pool[watcherType]
	r.WatcherPool.Mutex.RUnlock()

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
func (r *SourcesController) updateWatcherResourceByIndex(watcherType ResourceTypeName, resourceIndex int, resource *unstructured.Unstructured) error {
	// Lock the WatcherPool mutex for reading
	r.WatcherPool.Mutex.RLock()
	watcher, exists := r.WatcherPool.Pool[watcherType]
	r.WatcherPool.Mutex.RUnlock()

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
func (r *SourcesController) deleteWatcherResourceByIndex(watcherType ResourceTypeName, resourceIndex int) error {
	// Lock the WatcherPool mutex for reading
	r.WatcherPool.Mutex.RLock()
	watcher, exists := r.WatcherPool.Pool[watcherType]
	r.WatcherPool.Mutex.RUnlock()

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
