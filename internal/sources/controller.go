package sources

import (
	"context"
	"fmt"
	"strings"
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

// SourcesControllerOptions represents the controller that triggers parallel watchers.
// These watchers are in charge of maintaining the pool of sources asked by the user in ClusterAdmissionPolicy objects.
// A source group is represented by GVRNN (Group + Version + Resource + Namespace + Name)
type SourcesController struct {
	// Kubernetes clients
	Client *dynamic.DynamicClient

	// options to modify the behavior of this SourcesController
	Options SourcesControllerOptions

	// Carried stuff
	WatcherPool WatcherPoolT
}

// Start launches the SourcesController and keeps it alive
// It kills the controller on application context death, and rerun the process when failed
func (r *SourcesController) Start(ctx context.Context) {
	logger := log.FromContext(ctx)

	for {
		select {
		case <-ctx.Done():
			logger.Info("SourcesController finished by context")
			return
		default:
			logger.Info("Starting SourcesController")
			r.reconcileWatchers(ctx)
		}

		time.Sleep(2 * time.Second)
	}
}

// reconcileWatchers launches a parallel process that launches
// watchers for resource types defined into the WatcherPool
func (r *SourcesController) reconcileWatchers(ctx context.Context) {
	logger := log.FromContext(ctx)

	for resourceType, resourceTypeWatcher := range r.WatcherPool.Pool {

		// TODO: Is this really needed or useful?
		// Check the existence of the resourceType into the WatcherPool.
		// Remember the controller.ClusterAdmissionPolicyController can remove watchers on garbage collection
		if _, resourceTypeFound := r.WatcherPool.Pool[resourceType]; !resourceTypeFound {
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
			if *(r.WatcherPool.Pool[resourceType].Started) == false {
				logger.Info(fmt.Sprintf("Impossible to start watcher for resource type: %s", resourceType))
			}
		}

		// Wait a bit to reduce the spam to machine resources
		time.Sleep(r.Options.WatchersDurationBetweenReconcileLoops)
	}
}

// watchType launches a watcher for a certain resource type, and trigger processing for each entering resource event
func (r *SourcesController) watchType(ctx context.Context, watchedType ResourceTypeName) {

	logger := log.FromContext(ctx)

	logger.Info(fmt.Sprintf("Watcher for '%s' has been started", watchedType))

	// Set ACK flag for watcher launching into the WatcherPool
	*(r.WatcherPool.Pool[watchedType].Started) = true
	defer func() {
		*(r.WatcherPool.Pool[watchedType].Started) = false
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
		<-*(r.WatcherPool.Pool[watchedType].StopSignal)
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
			r.CreateWatcherResource(watchedType, convertedObject)
		},
		UpdateFunc: func(_, eventObject interface{}) {
			convertedObject := eventObject.(*unstructured.Unstructured)
			objectIndex := r.GetWatcherResourceIndex(watchedType, convertedObject)

			if objectIndex > -1 {
				r.UpdateWatcherResourceByIndex(watchedType, objectIndex, convertedObject)
			}
		},
		DeleteFunc: func(eventObject interface{}) {
			convertedObject := eventObject.(*unstructured.Unstructured)
			objectIndex := r.GetWatcherResourceIndex(watchedType, convertedObject)

			if objectIndex > -1 {
				r.DeleteWatcherResourceByIndex(watchedType, objectIndex)
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