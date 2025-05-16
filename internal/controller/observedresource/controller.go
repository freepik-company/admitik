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

package observedresource

import (
	"context"
	"fmt"
	"golang.org/x/exp/maps"
	"slices"
	"strings"
	"time"

	//
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"freepik.com/admitik/internal/globals"
	clusterGenerationPolicyRegistry "freepik.com/admitik/internal/registry/clustergenerationpolicy"
	resourceInformerRegistry "freepik.com/admitik/internal/registry/resourceinformer"
	resourceObserverRegistry "freepik.com/admitik/internal/registry/resourceobserver"
	sourcesRegistry "freepik.com/admitik/internal/registry/sources"
)

const (
	// secondsToCheckInformerAck is the number of seconds before checking
	// whether an informer is started or not during informers' reconciling process
	secondsToCheckInformerAck = 10 * time.Second

	// secondsToReconcileInformersAgain is the number of seconds to wait
	// between the moment of launching informers, and repeating this process
	// (avoid the spam, mate)
	secondsToReconcileInformersAgain = 2 * time.Second

	//
	controllerName = "observedresource"

	//
	controllerContextFinishedMessage = "ObservedResourceController finished by context"
	controllerInformerStartedMessage = "Informer for '%s' has been started"
	controllerInformerKilledMessage  = "Informer for resource type '%s' killed by StopSignal"

	watchedObjectParseError         = "Impossible to process triggered object: %s"
	resourceInformerLaunchingError  = "Impossible to start informer for resource type: %s"
	resourceInformerGvrParsingError = "Failed to parse GVR from resourceType. Does it look like {group}/{version}/{resource}/{namespace}/{name}?"
)

// ObservedResourceControllerOptions represents available options that can be passed to ObservedResourceController on start
type ObservedResourceControllerOptions struct {
	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration
}

type ObservedResourceControllerDependencies struct {
	Context *context.Context

	//
	ClusterGenerationPolicyRegistry *clusterGenerationPolicyRegistry.ClusterGenerationPolicyRegistry
	SourcesRegistry                 *sourcesRegistry.SourcesRegistry
	ResourceInformerRegistry        *resourceInformerRegistry.ResourceInformerRegistry
	ResourceObserverRegistry        *resourceObserverRegistry.ResourceObserverRegistry
}

// ObservedResourceController represents the controller that triggers parallel threads.
// These threads process coming events against the conditions defined in TODO
// Each thread is an informer in charge of a group of resources GVRNN (Group + Version + Resource + Namespace + Name)
type ObservedResourceController struct {
	Client client.Client

	Options      ObservedResourceControllerOptions
	Dependencies ObservedResourceControllerDependencies

	// Carried stuff
	kubeAvailableResourceList *[]GVKR // TODO: Time to wrap processors?
	dispatcher                *EventDispatcher
}

// getResourcesFromPolicyRegistries returns a list of observers for each type of resource that is required to be watched
// Example: map[resourceType][]Observers
// TODO: Improve docs
func (r *ObservedResourceController) getResourcesFromPolicyRegistries() map[string][]string {

	results := make(map[string][]string)

	candidatesFromGeneration := r.Dependencies.ClusterGenerationPolicyRegistry.GetRegisteredResourceTypes()
	for _, resourceType := range candidatesFromGeneration {
		if !slices.Contains(results[resourceType], ObserverTypeClusterGenerationPolicies) {
			results[resourceType] = append(results[resourceType], ObserverTypeClusterGenerationPolicies)
		}
	}

	// TODO: Register potential future CRs here

	return results
}

// informersCleanerWorker review the TODO
// It disables the informers that are not needed and delete them from ResourcesRegistry
// This function is intended to be used as goroutine
func (r *ObservedResourceController) informersCleanerWorker() {
	logger := log.FromContext(*r.Dependencies.Context)
	logger = logger.WithValues("controller", controllerName)

	logger.Info("Starting informers cleaner worker")

	for {
		referentCandidates := r.getResourcesFromPolicyRegistries()
		referentCandidatesKeys := maps.Keys(referentCandidates)

		reviewedCandidates := r.Dependencies.ResourceInformerRegistry.GetRegisteredResourceTypes()

		for _, resourceType := range reviewedCandidates {
			if !slices.Contains(referentCandidatesKeys, resourceType) {
				err := r.Dependencies.ResourceInformerRegistry.DisableInformer(resourceType)
				if err != nil {
					logger.WithValues("resourceType", resourceType).
						Info("Failed disabling watcher informer")
				}
			}
		}

		time.Sleep(5 * time.Second)
	}
}

// Start launches the ObservedResourceController and keeps it alive
// It kills the controller on application's context death, and rerun the process when failed
func (r *ObservedResourceController) Start() {
	logger := log.FromContext(*r.Dependencies.Context)
	logger = logger.WithValues("controller", controllerName)

	// Create an event dispatcher for later usage
	r.dispatcher = NewEventDispatcher(EventDispatcherDependencies{
		ClusterGenerationPolicyRegistry: r.Dependencies.ClusterGenerationPolicyRegistry,
		SourcesRegistry:                 r.Dependencies.SourcesRegistry,
		ResourceObserverRegistry:        r.Dependencies.ResourceObserverRegistry,
	})

	// Start cleaner for dead informers
	go r.informersCleanerWorker()

	// Keep your controller alive
	for {
		select {
		case <-(*r.Dependencies.Context).Done():
			logger.Info(controllerContextFinishedMessage)
			return
		default:
			r.reconcileInformers()
			time.Sleep(secondsToReconcileInformersAgain)
		}
	}
}

// reconcileInformers checks each registered 'watchedResource' type and triggers informers
// for those that are not already started.
func (r *ObservedResourceController) reconcileInformers() {
	logger := log.FromContext(*r.Dependencies.Context)
	logger = logger.WithValues("controller", controllerName)

	watcherCandidates := r.getResourcesFromPolicyRegistries()

	for resourceType, observers := range watcherCandidates {

		_, informerExists := r.Dependencies.ResourceInformerRegistry.GetInformer(resourceType)

		// Store the observers needing this informer
		if informerExists {
			r.Dependencies.ResourceObserverRegistry.SetObservers(resourceType, observers)
		}

		// Avoid wasting CPU for nothing
		if informerExists && r.Dependencies.ResourceInformerRegistry.IsStarted(resourceType) {
			continue
		}

		//
		if !informerExists || !r.Dependencies.ResourceInformerRegistry.IsStarted(resourceType) {
			go r.launchInformerForType(resourceType)

			// Wait for the just started informer to ACK itself
			time.Sleep(secondsToCheckInformerAck)
			if !r.Dependencies.ResourceInformerRegistry.IsStarted(resourceType) {
				logger.Info(fmt.Sprintf(resourceInformerLaunchingError, resourceType))
			}
		}
	}
}

// launchInformerForType creates and runs a Kubernetes informer for the specified
// resource type, and triggers processing for each event
func (r *ObservedResourceController) launchInformerForType(resourceType string) {
	logger := log.FromContext(*r.Dependencies.Context)
	logger = logger.WithValues("controller", controllerName)

	informer, informerExists := r.Dependencies.ResourceInformerRegistry.GetInformer(resourceType)
	if !informerExists {
		informer = r.Dependencies.ResourceInformerRegistry.RegisterInformer(resourceType)
	}

	logger.Info(fmt.Sprintf(controllerInformerStartedMessage, resourceType))

	// Trigger ACK flag for informer that is launching
	// Hey, this informer is blocking, so ACK is only disabled if the informer becomes dead
	_ = r.Dependencies.ResourceInformerRegistry.SetStarted(resourceType, true)
	defer func() {
		_ = r.Dependencies.ResourceInformerRegistry.SetStarted(resourceType, false)
	}()

	// Extract GVR + Namespace + Name from watched type:
	// {group}/{version}/{resource}/{namespace}/{name}
	GVRNN := strings.Split(resourceType, "/")
	if len(GVRNN) != 5 {
		logger.Info(resourceInformerGvrParsingError)
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

	// Listen to stop signal to kill this informer just in case it's needed
	stopCh := make(chan struct{})

	go func() {
		<-informer.StopSignal
		close(stopCh)
		logger.Info(fmt.Sprintf(controllerInformerKilledMessage, resourceType))
	}()

	// Define our informer TODO
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(globals.Application.KubeRawClient,
		r.Options.InformerDurationToResync, namespace, listOptionsFunc)

	// Create an informer. This is a special type of client-go informer that includes
	// mechanisms to hide disconnections, handle reconnections, and cache watched objects
	kubeInformer := factory.ForResource(resourceGVR).Informer()

	// Register functions to handle different types of events
	handlers := cache.ResourceEventHandlerFuncs{

		AddFunc: func(eventObject interface{}) {
			convertedEventObject := eventObject.(*unstructured.Unstructured)

			r.dispatcher.Dispatch(resourceType, watch.Added, convertedEventObject.UnstructuredContent())
		},
		UpdateFunc: func(eventObjectOld, eventObject interface{}) {
			convertedEventObjectOld := eventObjectOld.(*unstructured.Unstructured)
			convertedEventObject := eventObject.(*unstructured.Unstructured)

			r.dispatcher.Dispatch(resourceType, watch.Modified,
				convertedEventObject.UnstructuredContent(), convertedEventObjectOld.UnstructuredContent())
		},
		DeleteFunc: func(eventObject interface{}) {
			convertedEventObject := eventObject.(*unstructured.Unstructured)

			r.dispatcher.Dispatch(resourceType, watch.Deleted, convertedEventObject.UnstructuredContent())
		},
	}

	_, err := kubeInformer.AddEventHandler(handlers)
	if err != nil {
		logger.Error(err, "Error adding handling functions for events to an informer")
		return
	}

	kubeInformer.Run(stopCh)
}
