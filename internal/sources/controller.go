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
	"fmt"
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
	Client *dynamic.DynamicClient

	// options to modify the behavior of this SourcesController
	Options SourcesControllerOptions

	// Carried stuff
	watcherPool WatcherPoolT
}

// TODO
func (r *SourcesController) init() {
	r.watcherPool = WatcherPoolT{
		Mutex: &sync.RWMutex{},
		Pool:  map[resourceTypeName]*resourceTypeWatcherT{},
	}
}

// Start launches the SourcesController and keeps it alive
// It kills the controller on application context death, and rerun the process when failed
func (r *SourcesController) Start(ctx context.Context) {
	logger := log.FromContext(ctx)

	r.init()

	for {
		select {
		case <-ctx.Done():
			logger.Info("SourcesController finished by context")
			return
		default:
			r.reconcileWatchers(ctx)
		}

		time.Sleep(2 * time.Second)
	}
}

// reconcileWatchers launches a parallel process that launches
// watchers for resource types defined into the WatcherPool
func (r *SourcesController) reconcileWatchers(ctx context.Context) {
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
			go r.watchType(ctx, resourceType)

			// Wait for the resourceType watcher to ACK itself into WatcherPool
			time.Sleep(r.Options.WatcherDurationToAck)
			if !*(r.watcherPool.Pool[resourceType].Started) {
				logger.Info(fmt.Sprintf("Impossible to start watcher for resource type: %s", resourceType))
			}
		}

		// Wait a bit to reduce the spam to machine resources
		time.Sleep(r.Options.WatchersDurationBetweenReconcileLoops)
	}
}

// watchType launches a watcher for a certain resource type, and trigger processing for each entering resource event
func (r *SourcesController) watchType(ctx context.Context, watchedType resourceTypeName) {

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
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(r.Client,
		r.Options.InformerDurationToResync, namespace, listOptionsFunc)

	// Create an informer. This is a special type of client-go watcher that includes
	// mechanisms to hide disconnections, handle reconnections, and cache watched objects
	informer := factory.ForResource(resourceGVR).Informer()

	// Register functions to handle different types of events
	handlers := cache.ResourceEventHandlerFuncs{

		AddFunc: func(eventObject interface{}) {
			convertedObject := eventObject.(*unstructured.Unstructured)

			err := r.createWatcherResource(watchedType, convertedObject)
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

			objectIndex := r.getWatcherResourceIndex(watchedType, convertedObject)
			if objectIndex > -1 {

				err := r.updateWatcherResourceByIndex(watchedType, objectIndex, convertedObject)
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
			objectIndex := r.getWatcherResourceIndex(watchedType, convertedObject)

			if objectIndex > -1 {
				err := r.deleteWatcherResourceByIndex(watchedType, objectIndex)
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
