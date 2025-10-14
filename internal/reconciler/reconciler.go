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

package reconciler

import (
	"context"
	"k8s.io/client-go/dynamic"
	"time"

	//
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	controllerContextFinishedMessage = "Controller finished by context"
	controllerInformerStartedMessage = "Informer for '%s' has been started"
	controllerInformerKilledMessage  = "Informer for resource type '%s' killed by StopSignal"

	watchedObjectParseError         = "Impossible to process triggered object: %s"
	resourceInformerLaunchingError  = "Impossible to start informer for resource type: %s"
	resourceInformerGvrParsingError = "Failed to parse GVR from resourceType. Does it look like {group}/{version}/{resource}/{namespace}/{name}?"
)

type EventType string

const (
	EventTypeAdded   EventType = "Added"
	EventTypeUpdated EventType = "Updated"
	EventTypeDeleted EventType = "Deleted"
)

// ReconcilerHandler define la interfaz que debes implementar
type ReconcilerHandler interface {
	Reconcile(eventType EventType, obj *unstructured.Unstructured, oldObj *unstructured.Unstructured) error
}

type ReconcilerOptions struct {
	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration

	//
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

type ReconcilerDependencies struct {
	Context *context.Context
	Client  *dynamic.DynamicClient

	//
	Handler ReconcilerHandler
}

type Reconciler struct {
	Options      ReconcilerOptions
	Dependencies ReconcilerDependencies
}

func (r *Reconciler) Launch() {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)

	resourceGVR := schema.GroupVersionResource{
		Group:    r.Options.GVR.Group,
		Version:  r.Options.GVR.Version,
		Resource: r.Options.GVR.Resource,
	}

	// Include the namespace when defined by the user (used as filter)
	namespace := corev1.NamespaceAll
	if r.Options.Namespace != "" {
		namespace = r.Options.Namespace
	}

	// Include the name when defined by the user (used as filter)
	var listOptionsFunc dynamicinformer.TweakListOptionsFunc = func(options *metav1.ListOptions) {}
	if r.Options.Name != "" {
		listOptionsFunc = func(options *metav1.ListOptions) {
			options.FieldSelector = "metadata.name=" + r.Options.Name
		}
	}

	// Listen to stop signal to kill this informer just in case it's needed
	stopCh := make(chan struct{})
	go func() {
		<-(*r.Dependencies.Context).Done()
		close(stopCh)
		logger.Info(controllerInformerKilledMessage,
			"gvr", r.Options.GVR,
			"namespace", r.Options.Namespace,
			"name", r.Options.Name,
		)
	}()

	// Define our informer TODO
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(r.Dependencies.Client,
		r.Options.InformerDurationToResync, namespace, listOptionsFunc)

	kubeInformer := factory.ForResource(resourceGVR).Informer()

	// Register functions to handle different types of events
	handlers := cache.ResourceEventHandlerFuncs{

		AddFunc: func(eventObject interface{}) {
			obj := eventObject.(*unstructured.Unstructured)

			logger = logger.WithValues(
				"eventType", EventTypeAdded,
				"object", obj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := r.Dependencies.Handler.Reconcile(EventTypeAdded, obj, nil); err != nil {
				logger.Error(err, "Error in add reconciliation")
			}
		},
		UpdateFunc: func(eventObjectOld, eventObject interface{}) {
			obj := eventObject.(*unstructured.Unstructured)
			oldObj := eventObjectOld.(*unstructured.Unstructured)

			logger = logger.WithValues(
				"eventType", EventTypeUpdated,
				"object", obj.GetName(),
				"oldObject", oldObj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := r.Dependencies.Handler.Reconcile(EventTypeUpdated, obj, oldObj); err != nil {
				logger.Error(err, "Error in update reconciliation")
			}
		},
		DeleteFunc: func(eventObject interface{}) {
			obj := eventObject.(*unstructured.Unstructured)

			logger = logger.WithValues(
				"eventType", EventTypeDeleted,
				"object", obj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := r.Dependencies.Handler.Reconcile(EventTypeDeleted, obj, nil); err != nil {
				logger.Error(err, "Error in delete reconciliation")
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
