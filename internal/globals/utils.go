package globals

import (

	//
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	//
)

// NewKubernetesClient return a new Kubernetes Dynamic client from client-go SDK
func NewKubernetesClient() (client *dynamic.DynamicClient, err error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return client, err
	}

	// Create the clients to do requests to our friend: Kubernetes
	client, err = dynamic.NewForConfig(config)
	if err != nil {
		return client, err
	}

	return client, err
}

// // TODO
// func InitWatcher(watcherType ResourceTypeName) {

// 	var initialStartedState bool = false
// 	var initialBlockedState bool = false
// 	var initialNotificationListState []*notifikv1alpha1.Notification

// 	initialStopSignalState := make(chan bool)
// 	initialMutexState := sync.Mutex{}

// 	Application.WatcherPool.Pool[watcherType] = ResourceTypeWatcherT{
// 		Mutex: &initialMutexState,

// 		Started:    &initialStartedState,
// 		Blocked:    &initialBlockedState,
// 		StopSignal: &initialStopSignalState,

// 		NotificationList: &initialNotificationListState,
// 	}
// }

// TODO
// func GetWatcherNotificationIndex(watcherType ResourceTypeName, notificationManifest *notifikv1alpha1.Notification) (result int) {

// 	notificationList := Application.WatcherPool.Pool[watcherType].NotificationList

// 	for notificationIndex, notification := range *notificationList {
// 		if (notification.Name == notificationManifest.Name) &&
// 			(notification.Namespace == notificationManifest.Namespace) {
// 			return notificationIndex
// 		}
// 	}

// 	return -1
// }

// TODO
// func GetWatcherPoolNotificationIndexes(notificationManifest *notifikv1alpha1.Notification) (result map[string]int) {

// 	result = make(map[string]int)

// 	for watcherType, _ := range Application.WatcherPool.Pool {
// 		notificationIndex := GetWatcherNotificationIndex(watcherType, notificationManifest)

// 		if notificationIndex != -1 {
// 			result[string(watcherType)] = notificationIndex
// 		}
// 	}

// 	return result
// }

// // TODO
// func CreateWatcherNotification(watcherType ResourceTypeName, notificationManifest *notifikv1alpha1.Notification) {

// 	notificationList := Application.WatcherPool.Pool[watcherType].NotificationList

// 	(Application.WatcherPool.Pool[watcherType].Mutex).Lock()

// 	temporaryManifest := (*notificationManifest).DeepCopy()
// 	*notificationList = append(*notificationList, temporaryManifest)

// 	(Application.WatcherPool.Pool[watcherType].Mutex).Unlock()
// }

// // TODO
// func DeleteWatcherNotificationByIndex(watcherType ResourceTypeName, notificationIndex int) {

// 	notificationList := Application.WatcherPool.Pool[watcherType].NotificationList

// 	(Application.WatcherPool.Pool[watcherType].Mutex).Lock()

// 	// Substitute the selected notification object with the last one from the list,
// 	// then replace the whole list with it, minus the last.
// 	//(*notificationList)[notificationIndex] = (*notificationList)[len(*notificationList)-1]
// 	//*notificationList = (*notificationList)[:len(*notificationList)-1]

// 	*notificationList = append((*notificationList)[:notificationIndex], (*notificationList)[notificationIndex+1:]...)

// 	(Application.WatcherPool.Pool[watcherType].Mutex).Unlock()
// }

// DisableWatcherFromWatcherPool disable a watcher from the WatcherPool.
// It first blocks the watcher to prevent it from being started by xyz.WorkloadController,
// then blocks the WatcherPool temporary while killing the watcher.
// func DisableWatcherFromWatcherPool(watcherType ResourceTypeName) (result bool, err error) {

// 	//Application.WatcherPool.Pool[watcherType].Mutex.Lock()

// 	// 1. Prevent watcher from being started again
// 	*Application.WatcherPool.Pool[watcherType].Blocked = true

// 	// 2. Stop the watcher
// 	*Application.WatcherPool.Pool[watcherType].StopSignal <- true

// 	//Application.WatcherPool.Pool[watcherType].Mutex.Unlock()

// 	// 3. Wait for the watcher to be stopped. Return false on failure
// 	stoppedWatcher := false
// 	for i := 0; i < 10; i++ {
// 		if !*Application.WatcherPool.Pool[watcherType].Started {
// 			stoppedWatcher = true
// 			break
// 		}
// 		time.Sleep(1 * time.Second)
// 	}

// 	if !stoppedWatcher {
// 		return false, errors.New("impossible to stop the watcher")
// 	}

// 	// 4. Delete the watcher from the WatcherPool.Pool
// 	//Application.WatcherPool.Mutex.Lock()
// 	//delete(Application.WatcherPool.Pool, watcherType)
// 	//Application.WatcherPool.Mutex.Unlock()

// 	//if _, keyFound := Application.WatcherPool.Pool[watcherType]; keyFound {
// 	//	return false, errors.New("impossible to delete the watcherType from WatcherPool")
// 	//}

// 	return true, nil
// }

// CleanWatcherPool check the WatcherPool looking for empty watchers to trigger their deletion.
// This function is intended to be executed on its own, so returns nothing
// func CleanWatcherPool() {
// 	logger := log.FromContext(Application.Context)

// 	for watcherType, _ := range Application.WatcherPool.Pool {

// 		if len(*Application.WatcherPool.Pool[watcherType].NotificationList) != 0 {
// 			continue
// 		}

// 		watcherDeleted, err := DisableWatcherFromWatcherPool(watcherType)
// 		if !watcherDeleted {
// 			logger.WithValues("watcher", watcherType, "error", err).
// 				Info("watcher was not deleted from WatcherPool")
// 			continue
// 		}

// 		logger.WithValues("watcher", watcherType).
// 			Info("watcher has been deleted from WatcherPool")
// 	}
// }
