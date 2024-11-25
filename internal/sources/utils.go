package sources

import (
	"errors"
	"fmt"
	"slices"
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

	var initialStartedState bool = false
	var initialBlockedState bool = false
	var initialResourceListState []*unstructured.Unstructured

	initialStopSignalState := make(chan bool)
	initialMutexState := sync.Mutex{}

	r.WatcherPool.Pool[watcherType] = ResourceTypeWatcherT{
		Mutex: &initialMutexState,

		Started:    &initialStartedState,
		Blocked:    &initialBlockedState,
		StopSignal: &initialStopSignalState,

		ResourceList: &initialResourceListState,
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

	*r.WatcherPool.Pool[watcherType].ResourceList = []*unstructured.Unstructured{}
	return true
}

// SyncWatchers ensures the WatcherPool matches the desired state.
//
// Given a list of desired watchers in GVRNN format (Group/Version/Resource/Namespace/Name),
// this function creates missing watchers, ensures active ones are unblocked, and removes
// any watchers that are no longer needed.
func (r *SourcesController) SyncWatchers(watcherTypeList []ResourceTypeName) (err error) {
	r.WatcherPool.Mutex.Lock()
	defer r.WatcherPool.Mutex.Unlock()

	// 1. Keep existing watchers (or create them) for desired resources types
	for _, desiredPoolName := range watcherTypeList {

		// Scaffold new watchers when they does not exist
		if _, exists := r.WatcherPool.Pool[desiredPoolName]; !exists {
			r.prepareWatcher(desiredPoolName)
			continue
		}

		// Ensure already existing ones are NOT blocked
		r.WatcherPool.Pool[desiredPoolName].Mutex.Lock()
		if !*r.WatcherPool.Pool[desiredPoolName].Started {
			falseVal := false
			*r.WatcherPool.Pool[desiredPoolName].Blocked = falseVal
		}
		r.WatcherPool.Pool[desiredPoolName].Mutex.Unlock()
	}

	// 2. Clean up unneeded watchers
	for existingPoolName, existingPool := range r.WatcherPool.Pool {

		if !slices.Contains(watcherTypeList, existingPoolName) {
			existingPool.Mutex.Lock()

			watcherDisabled := r.disableWatcher(existingPoolName)
			if !watcherDisabled {
				err = errors.Join(fmt.Errorf("impossible to disable watcher for: %s", existingPoolName))
			}

			existingPool.Mutex.Unlock()

			// Clean up the watcher from the pool
			delete(r.WatcherPool.Pool, existingPoolName)
			continue
		}
	}

	return err
}

// TODO
func (r *SourcesController) createWatcherResource(watcherType ResourceTypeName, resource *unstructured.Unstructured) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	(r.WatcherPool.Pool[watcherType].Mutex).Lock()
	defer (r.WatcherPool.Pool[watcherType].Mutex).Unlock()

	temporaryManifest := (*resource).DeepCopy()
	*resourceList = append(*resourceList, temporaryManifest)
}

// TODO
func (r *SourcesController) getWatcherResourceIndex(watcherType ResourceTypeName, resource *unstructured.Unstructured) (result int) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	for tmpResourceIndex, tmpResource := range *resourceList {

		if (tmpResource.GetName() == resource.GetName()) &&
			(tmpResource.GetNamespace() == resource.GetNamespace()) {
			return tmpResourceIndex
		}
	}

	return -1
}

// TODO
func (r *SourcesController) updateWatcherResourceByIndex(watcherType ResourceTypeName, resourceIndex int, resource *unstructured.Unstructured) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	(r.WatcherPool.Pool[watcherType].Mutex).Lock()
	defer (r.WatcherPool.Pool[watcherType].Mutex).Unlock()

	(*resourceList)[resourceIndex] = resource
}

// TODO
func (r *SourcesController) deleteWatcherResourceByIndex(watcherType ResourceTypeName, resourceIndex int) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	(r.WatcherPool.Pool[watcherType].Mutex).Lock()
	defer (r.WatcherPool.Pool[watcherType].Mutex).Unlock()

	// Substitute the selected notification object with the last one from the list,
	// then replace the whole list with it, minus the last.
	*resourceList = append((*resourceList)[:resourceIndex], (*resourceList)[resourceIndex+1:]...)
}
