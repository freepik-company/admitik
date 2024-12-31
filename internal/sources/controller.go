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
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	//

	//
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SourcesControllerOptions TODO
type SourcesControllerOptions struct {

	// Kubernetes clients
	Client *dynamic.DynamicClient

	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration

	// WatchersDurationBetweenReconcileLoops is the duration to wait between the moment
	// of launching watchers, and repeating this process (avoid the spam, mate)
	WatchersDurationBetweenReconcileLoops time.Duration

	// WatcherDurationToAck is the duration before checking whether a watcher
	// is started or not during watchers' reconciling process
	WatcherDurationToAck time.Duration
}

// SourcesController represents the controller that triggers parallel watchers.
// These watchers are in charge of maintaining the pool of sources asked by the user in Policy objects.
// A source group is represented by GVRNN (Group + Version + Resource + Namespace + Name)
type SourcesController struct {
	// Kubernetes clients
	//client *dynamic.DynamicClient

	// options to modify the behavior of this SourcesController
	options SourcesControllerOptions

	// Carried stuff
	watcherPool WatcherPoolT
}

func NewSourcesController(options SourcesControllerOptions) *SourcesController {

	sourcesController := &SourcesController{
		options: options,
		watcherPool: WatcherPoolT{
			Mutex: &sync.RWMutex{},
			Pool:  map[resourceTypeName]*resourceTypeWatcherT{},
		},
	}

	return sourcesController
}

// Start launches the SourcesController main loop, which continuously monitors and starts
// eligible watchers until the context is cancelled.
func (r *SourcesController) Start(ctx context.Context) {
	logger := log.FromContext(ctx)

	//r.init()

	for {
		select {
		case <-ctx.Done():
			logger.Info("SourcesController finished by context")
			return
		default:
			r.launchEligibleWatchers(ctx)
		}

		time.Sleep(2 * time.Second)
	}
}

// launchEligibleWatchers starts watchers for resource types defined into the WatcherPool.
// It launches watchers that are neither blocked nor already running.
// It launches each eligible watcher in a separate goroutine and waits for their acknowledgment.
func (r *SourcesController) launchEligibleWatchers(ctx context.Context) {
	logger := log.FromContext(ctx)

	for resourceType, resourceTypeWatcher := range r.watcherPool.Pool {

		// TODO: Is this really needed or useful?
		// Check the existence of the resourceType into the WatcherPool.
		// Remember the controller.ClusterAdmissionPolicyController can remove watchers on garbage collection
		if _, resourceTypeFound := r.watcherPool.Pool[resourceType]; !resourceTypeFound {
			continue
		}

		// Prevent blocked watchers from being started.
		// Remember the controller.ClusterAdmissionPolicyController blocks them during garbage collection
		if *resourceTypeWatcher.Blocked {
			continue
		}

		if !*resourceTypeWatcher.Started {
			go r.startResourceTypeWatcher(ctx, resourceType)

			// Wait for the resourceType watcher to ACK itself into WatcherPool
			time.Sleep(r.options.WatcherDurationToAck)
			if !*(r.watcherPool.Pool[resourceType].Started) {
				logger.Info(fmt.Sprintf("Impossible to start watcher for resource type: %s", resourceType))
			}
		}

		// Wait a bit to reduce the spam to machine resources
		time.Sleep(r.options.WatchersDurationBetweenReconcileLoops)
	}
}

// startResourceTypeWatcher starts a dynamic informer that watches a specific resource type identified by its GVR/namespace/name pattern.
// It handles resource events (Add/Update/Delete) to maintain a synchronized list of resources in the watcher pool.
// The watcher runs until either the context is cancelled or a stop signal is received.
//
// The watchedType parameter must follow the format: "group/version/resource/namespace/name"
// where namespace and name are optional filters (use empty string for no filter).
func (r *SourcesController) startResourceTypeWatcher(ctx context.Context, watchedType resourceTypeName) {

	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Watcher for '%s' has been started", watchedType))

	// Set ACK flag for watcher launching into the WatcherPool
	*(r.watcherPool.Pool[watchedType].Started) = true
	defer func() {
		*(r.watcherPool.Pool[watchedType].Started) = false
	}()

	// Extract GVR + Namespace + Name from watched type:
	// {group}/{version}/{resource}/{namespace}/{name}
	GVRNN := strings.Split(string(watchedType), "/")
	if len(GVRNN) != 5 {
		logger.Info("Failed to parse GVR from resourceType. Does it look like {group}/{version}/{resource}?")
		return
	}
	resourceGVR := schema.GroupVersionResource{
		Group:    GVRNN[0],
		Version:  GVRNN[1],
		Resource: GVRNN[2],
	}

	// Include the namespace when defined by the user (used as filter)
	namespace := corev1.NamespaceAll
	if GVRNN[3] != "" {
		namespace = GVRNN[3]
	}

	// Include the name when defined by the user (used as filter)
	name := GVRNN[4]

	var listOptionsFunc dynamicinformer.TweakListOptionsFunc = func(options *metav1.ListOptions) {}
	if name != "" {
		listOptionsFunc = func(options *metav1.ListOptions) {
			options.FieldSelector = "metadata.name=" + name
		}
	}

	// Listen to stop signal to kill this watcher just in case it's needed
	stopCh := make(chan struct{})

	go func() {
		<-*(r.watcherPool.Pool[watchedType].StopSignal)
		close(stopCh)
		logger.Info(fmt.Sprintf("Watcher for resource type '%s' killed by StopSignal", watchedType))
	}()

	// Define our informer
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(r.options.Client,
		r.options.InformerDurationToResync, namespace, listOptionsFunc)

	// Create an informer. This is a special type of client-go watcher that includes
	// mechanisms to hide disconnections, handle reconnections, and cache watched objects
	informer := factory.ForResource(resourceGVR).Informer()

	// Register functions to handle different types of events
	handlers := cache.ResourceEventHandlerFuncs{

		AddFunc: func(eventObject interface{}) {
			convertedObject := eventObject.(*unstructured.Unstructured)

			err := r.watcherPool.createWatcherResource(watchedType, convertedObject)
			if err != nil {
				logger.WithValues(
					"watcher", watchedType,
					"object", convertedObject.GetNamespace()+"/"+convertedObject.GetName(),
				).Error(err, "Error creating resource in resource list")
				return
			}
		},
		UpdateFunc: func(_, eventObject interface{}) {
			convertedObject := eventObject.(*unstructured.Unstructured)

			objectIndex := r.watcherPool.getWatcherResourceIndex(watchedType, convertedObject)
			if objectIndex > -1 {

				err := r.watcherPool.updateWatcherResourceByIndex(watchedType, objectIndex, convertedObject)
				if err != nil {
					logger.WithValues(
						"watcher", watchedType,
						"object", convertedObject.GetNamespace()+"/"+convertedObject.GetName(),
					).Error(err, "Error updating resource in resource list")
					return
				}
			}
		},
		DeleteFunc: func(eventObject interface{}) {
			convertedObject := eventObject.(*unstructured.Unstructured)
			objectIndex := r.watcherPool.getWatcherResourceIndex(watchedType, convertedObject)

			if objectIndex > -1 {
				err := r.watcherPool.deleteWatcherResourceByIndex(watchedType, objectIndex)
				if err != nil {
					logger.WithValues(
						"watcher", watchedType,
						"object", convertedObject.GetNamespace()+"/"+convertedObject.GetName(),
					).Error(err, "Error deleting resource from resource list")
					return
				}
			}
		},
	}

	_, err := informer.AddEventHandler(handlers)
	if err != nil {
		logger.Error(err, "Error adding handling functions for events to an informer")
		return
	}

	informer.Run(stopCh)
}

// SyncWatchers ensures the WatcherPool matches the desired state.
//
// Given a list of desired watchers in GVRNN format (Group/Version/Resource/Namespace/Name),
// this function creates missing watchers, ensures active ones are unblocked, and removes
// any watchers that are no longer needed.
// TODO: look for a better function name. ReconcileRequesterTypes? SyncRequesterTypes?
func (r *SourcesController) SyncWatchers(watcherTypeList []string, requester string) (err error) {

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

		// Create it when is not already created
		if !exists {
			r.watcherPool.Mutex.Lock()
			r.watcherPool.prepareWatcher(watcherType)
			r.watcherPool.Pool[watcherType].Requesters = append(r.watcherPool.Pool[watcherType].Requesters, requester)
			r.watcherPool.Mutex.Unlock()
			continue
		}

		// Ensure having requester's finalizer for already existing ones
		watcher.Mutex.Lock()
		if !slices.Contains(watcher.Requesters, requester) {
			watcher.Requesters = append(watcher.Requesters, requester)
		}
		watcher.Mutex.Unlock()

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

			// Delete the requester from watchers
			watcher.Requesters = slices.DeleteFunc(watcher.Requesters, func(finalizer string) bool {
				return finalizer == requester
			})

			// Ignore deletion for those watchers that are still requested by other controllers
			if len(watcher.Requesters) > 0 {
				continue
			}

			watcher.Mutex.Lock()
			watcherDisabled := r.watcherPool.disableWatcher(watcherType)
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

// GetWatcherResources returns all resources being watched for a specific watcher type.
// Takes a watcherType in format "group/version/resource/namespace/name".
// Thread-safe using read locks for pool and resource list access.
// Returns error if pool not initialized or watcher type not found.
// TODO: look for a better function name. GetResourcesForType?
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
