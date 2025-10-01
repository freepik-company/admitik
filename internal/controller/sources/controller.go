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
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//
	"github.com/freepik-company/admitik/internal/globals"
	clusterGenerationPolicyRegistry "github.com/freepik-company/admitik/internal/registry/clustergenerationpolicy"
	clusterMutationPolicyRegistry "github.com/freepik-company/admitik/internal/registry/clustermutationpolicy"
	clusterValidationPolicyRegistry "github.com/freepik-company/admitik/internal/registry/clustervalidationpolicy"
	sourcesRegistry "github.com/freepik-company/admitik/internal/registry/sources"
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
	controllerName = "sources"

	//
	controllerContextFinishedMessage = "Controller finished by context"
	controllerInformerStartedMessage = "Informer for '%s' has been started"
	controllerInformerKilledMessage  = "Informer for resource type '%s' killed by StopSignal"

	watchedObjectParseError         = "Impossible to process triggered object: %s"
	resourceInformerLaunchingError  = "Impossible to start informer for resource type: %s"
	resourceInformerGvrParsingError = "Failed to parse GVR from resourceType. Does it look like {group}/{version}/{resource}?"
)

// SourcesControllerOptions represents available options that can be passed to SourcesController on start
type SourcesControllerOptions struct {
	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration
}

type SourcesControllerDependencies struct {
	Context *context.Context

	//
	ClusterGenerationPolicyRegistry *clusterGenerationPolicyRegistry.ClusterGenerationPolicyRegistry
	ClusterMutationPolicyRegistry   *clusterMutationPolicyRegistry.ClusterMutationPolicyRegistry
	ClusterValidationPolicyRegistry *clusterValidationPolicyRegistry.ClusterValidationPolicyRegistry
	SourcesRegistry                 *sourcesRegistry.SourcesRegistry
}

// SourcesController represents a controller that triggers parallel threads.
// These threads watch resources defined in 'sources' section of several object types stored in registries.
// Each thread is an informer in charge of a group of resources GVRNN (Group + Version + Resource + Namespace + Name)
type SourcesController struct {
	// Following interface is just needed to register this controller into Controller Runtime manager and let it
	// launch the controller across all the Admitik replicas or just in the elected leader.
	manager.LeaderElectionRunnable

	//
	Client client.Client

	Options      SourcesControllerOptions
	Dependencies SourcesControllerDependencies
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// This is needed to inform Controller Runtime manager whether this controller needs a leader or not.
func (r *SourcesController) NeedLeaderElection() bool {
	return false
}

// getSourcesFromRegistries returns a list of sources with all the types registered in suitable registries
func (r *SourcesController) getSourcesFromRegistries() []string {

	var referentCandidates []string

	candidatesFromGeneration := r.Dependencies.ClusterGenerationPolicyRegistry.GetRegisteredSourcesTypes()
	candidatesFromMutation := r.Dependencies.ClusterMutationPolicyRegistry.GetRegisteredSourcesTypes()
	candidatesFromValidation := r.Dependencies.ClusterValidationPolicyRegistry.GetRegisteredSourcesTypes()
	referentCandidates = slices.Concat(candidatesFromGeneration, candidatesFromMutation, candidatesFromValidation)

	// Filter duplicated items
	slices.Sort(referentCandidates)
	referentCandidates = slices.Compact(referentCandidates)

	return referentCandidates
}

// informersCleanerWorker review the 'sources' section of several object types stored in registries in the background.
// It disables the informers that are not needed and delete them from sources registry
// This function is intended to be used as goroutine
func (r *SourcesController) informersCleanerWorker() {
	logger := log.FromContext(*r.Dependencies.Context)
	logger = logger.WithValues("controller", controllerName)

	logger.Info("Starting Worker", "worker", "InformersCleaner")

	for {
		//
		referentCandidates := r.getSourcesFromRegistries()
		reviewedCandidates := r.Dependencies.SourcesRegistry.GetRegisteredResourceTypes()

		for _, resourceType := range reviewedCandidates {
			if !slices.Contains(referentCandidates, resourceType) {
				err := r.Dependencies.SourcesRegistry.DisableInformer(resourceType)
				if err != nil {
					logger.WithValues("resourceType", resourceType).
						Info("Failed disabling sources informer")
				}
			}
		}

		time.Sleep(5 * time.Second)
	}
}

// Start launches the SourcesController and keeps it alive
// It kills the controller on application's context death, and rerun the process when failed
func (r *SourcesController) Start(ctx context.Context) error {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)
	logger.Info("Starting Controller")

	// Start cleaner for dead informers
	go r.informersCleanerWorker()

	// Keep your controller alive
	for {
		select {
		case <-(*r.Dependencies.Context).Done():
			logger.Info(controllerContextFinishedMessage)
			return nil
		default:
			r.reconcileInformers()
			time.Sleep(secondsToReconcileInformersAgain)
		}
	}
}

// reconcileInformers checks each registered extra-resource type and triggers informers
// for those that are not already started.
func (r *SourcesController) reconcileInformers() {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)

	sourcesCandidates := r.getSourcesFromRegistries()

	for _, resourceType := range sourcesCandidates {

		_, informerExists := r.Dependencies.SourcesRegistry.GetInformer(resourceType)

		// Avoid wasting CPU for nothing
		if informerExists && r.Dependencies.SourcesRegistry.IsStarted(resourceType) {
			continue
		}

		//
		if !informerExists || !r.Dependencies.SourcesRegistry.IsStarted(resourceType) {
			go r.launchInformerForType(resourceType)

			// Wait for the just started informer to ACK itself
			time.Sleep(secondsToCheckInformerAck)
			if !r.Dependencies.SourcesRegistry.IsStarted(resourceType) {
				logger.Info(fmt.Sprintf(resourceInformerLaunchingError, resourceType))
			}
		}
	}
}

// launchInformerForType creates and runs a Kubernetes informer for the specified
// resource type, and triggers processing for each event
func (r *SourcesController) launchInformerForType(resourceType sourcesRegistry.ResourceTypeName) {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)

	informer, informerExists := r.Dependencies.SourcesRegistry.GetInformer(resourceType)
	if !informerExists {
		informer = r.Dependencies.SourcesRegistry.RegisterInformer(resourceType)
	}

	logger.Info(fmt.Sprintf(controllerInformerStartedMessage, resourceType))

	// Trigger ACK flag for informer that is launching
	// Hey, this informer is blocking, so ACK is only disabled if the informer becomes dead
	_ = r.Dependencies.SourcesRegistry.SetStarted(resourceType, true)
	defer func() {
		_ = r.Dependencies.SourcesRegistry.SetStarted(resourceType, false)
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

			err := r.processEvent(resourceType, watch.Added, convertedEventObject.UnstructuredContent())
			if err != nil {
				logger.Error(err, fmt.Sprintf(watchedObjectParseError, err))
			}
		},
		UpdateFunc: func(eventObjectOld, eventObject interface{}) {
			convertedEventObjectOld := eventObjectOld.(*unstructured.Unstructured)
			convertedEventObject := eventObject.(*unstructured.Unstructured)

			err := r.processEvent(resourceType, watch.Modified,
				convertedEventObject.UnstructuredContent(), convertedEventObjectOld.UnstructuredContent())
			if err != nil {
				logger.Error(err, fmt.Sprintf(watchedObjectParseError, err))
			}
		},
		DeleteFunc: func(eventObject interface{}) {
			convertedEventObject := eventObject.(*unstructured.Unstructured)

			err := r.processEvent(resourceType, watch.Deleted, convertedEventObject.UnstructuredContent())
			if err != nil {
				logger.Error(err, fmt.Sprintf(watchedObjectParseError, err))
			}
		},
	}

	_, err := kubeInformer.AddEventHandler(handlers)
	if err != nil {
		logger.Error(err, "Error adding handling functions for events to an informer")
		return
	}

	kubeInformer.Run(stopCh)
}

// processEvent process an event coming from a triggered extra-resource type.
// It reconciles stored resources in an idempotent way
func (r *SourcesController) processEvent(resourceType sourcesRegistry.ResourceTypeName, eventType watch.EventType, object ...map[string]interface{}) (err error) {

	// Process only certain event types
	if eventType != watch.Added && eventType != watch.Modified && eventType != watch.Deleted {
		return nil
	}

	if eventType == watch.Deleted {
		err = r.Dependencies.SourcesRegistry.RemoveResource(resourceType, &object[0])
		return err
	}

	// Create/Update events
	if eventType == watch.Modified {
		err = r.Dependencies.SourcesRegistry.RemoveResource(resourceType, &object[0])
		if err != nil {
			return err
		}
	}

	r.Dependencies.SourcesRegistry.AddResource(resourceType, &object[0])

	return err
}
