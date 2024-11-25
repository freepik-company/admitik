package sources

import (
	"errors"
	"sync"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	//
)

// TODO
func (r *SourcesController) StartWatcher(watcherType ResourceTypeName) {

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

// TODO
func (r *SourcesController) CreateWatcherResource(watcherType ResourceTypeName, resource *unstructured.Unstructured) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	(r.WatcherPool.Pool[watcherType].Mutex).Lock()
	defer (r.WatcherPool.Pool[watcherType].Mutex).Unlock()

	temporaryManifest := (*resource).DeepCopy()
	*resourceList = append(*resourceList, temporaryManifest)
}

// TODO
func (r *SourcesController) GetWatcherResourceIndex(watcherType ResourceTypeName, resource *unstructured.Unstructured) (result int) {

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
func (r *SourcesController) UpdateWatcherResourceByIndex(watcherType ResourceTypeName, resourceIndex int, resource *unstructured.Unstructured) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	(r.WatcherPool.Pool[watcherType].Mutex).Lock()
	defer (r.WatcherPool.Pool[watcherType].Mutex).Unlock()

	(*resourceList)[resourceIndex] = resource
}

// TODO
func (r *SourcesController) DeleteWatcherResourceByIndex(watcherType ResourceTypeName, resourceIndex int) {

	resourceList := r.WatcherPool.Pool[watcherType].ResourceList

	(r.WatcherPool.Pool[watcherType].Mutex).Lock()
	defer (r.WatcherPool.Pool[watcherType].Mutex).Unlock()

	// Substitute the selected notification object with the last one from the list,
	// then replace the whole list with it, minus the last.
	*resourceList = append((*resourceList)[:resourceIndex], (*resourceList)[resourceIndex+1:]...)
}

// DisableWatcherFromWatcherPool disable a watcher from the WatcherPool.
// It first blocks the watcher to prevent it from being started by any controller,
// then blocks the WatcherPool temporary while killing the watcher.
func (r *SourcesController) DisableWatcherFromWatcherPool(watcherType ResourceTypeName) (result bool, err error) {

	//Application.WatcherPool.Pool[watcherType].Mutex.Lock()

	// 1. Prevent watcher from being started again
	*r.WatcherPool.Pool[watcherType].Blocked = true

	// 2. Stop the watcher
	*r.WatcherPool.Pool[watcherType].StopSignal <- true

	//Application.WatcherPool.Pool[watcherType].Mutex.Unlock()

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
		return false, errors.New("impossible to stop the watcher")
	}

	// 4. Delete the watcher from the WatcherPool.Pool
	//Application.WatcherPool.Mutex.Lock()
	//delete(Application.WatcherPool.Pool, watcherType)
	//Application.WatcherPool.Mutex.Unlock()

	//if _, keyFound := Application.WatcherPool.Pool[watcherType]; keyFound {
	//	return false, errors.New("impossible to delete the watcherType from WatcherPool")
	//}

	return true, nil
}

// CleanWatcherPool check the WatcherPool looking for empty watchers to trigger their deletion.
// This function is intended to be executed on its own, so returns nothing
// func (r *SourcesController) CleanWatcherPool(ctx context.Context) {
// 	logger := log.FromContext(ctx)

// 	for watcherType, _ := range r.WatcherPool.Pool {

// 		if len(*r.WatcherPool.Pool[watcherType].ResourceList) != 0 {
// 			continue
// 		}

// 		watcherDeleted, err := r.DisableWatcherFromWatcherPool(watcherType)
// 		if !watcherDeleted {
// 			logger.WithValues("watcher", watcherType, "error", err).
// 				Info("watcher was not deleted from WatcherPool")
// 			continue
// 		}

// 		logger.WithValues("watcher", watcherType).
// 			Info("watcher has been deleted from WatcherPool")
// 	}
// }
