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

package informer

import (
	"context"
	"fmt"
	"k8s.io/client-go/dynamic"
	"sync"
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
	defaultResyncPeriod = 10 * time.Minute
	cacheWaitTimeout    = 30 * time.Second

	//
	informerContextFinishedMessage = "Informer finished by context"
	informerStartedMessage         = "Informer for '%s' has been started"
	informerKilledMessage          = "Informer for resource type '%s' killed by StopSignal"
)

type EventType string

const (
	EventTypeAdded   EventType = "Added"
	EventTypeUpdated EventType = "Updated"
	EventTypeDeleted EventType = "Deleted"
)

type EventHandlerFunc = func(eventType EventType, obj *unstructured.Unstructured, oldObj *unstructured.Unstructured) error

type Options struct {
	//
	GVR       schema.GroupVersionResource
	Namespace string

	InformerDurationToResync time.Duration

	// Optional: additional filters
	LabelSelector string
	FieldSelector string
}

type Dependencies struct {
	Context context.Context
	Client  *dynamic.DynamicClient
}

type Informer struct {
	options      Options
	dependencies Dependencies

	//
	rawInformer cache.SharedIndexInformer
	stopChan    chan struct{}

	//
	mu      sync.RWMutex
	started bool
}

func NewInformer(opts Options, deps Dependencies) (*Informer, error) {

	if deps.Client == nil {
		return nil, fmt.Errorf("kube client cannot be nil")
	}

	if deps.Context == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}

	if opts.InformerDurationToResync == 0 {
		opts.InformerDurationToResync = defaultResyncPeriod
	}

	tmpInformer := &Informer{
		options:      opts,
		dependencies: deps,
		stopChan:     make(chan struct{}),
		started:      false,
	}

	//
	tmpInformer.createInformer()
	return tmpInformer, nil
}

func (r *Informer) createInformer() {
	// Include the namespace when defined by the user (used as filter)
	namespace := corev1.NamespaceAll
	if r.options.Namespace != "" {
		namespace = r.options.Namespace
	}

	// Include the name when defined by the user (used as filter)
	var tweakListOptions dynamicinformer.TweakListOptionsFunc
	if r.options.LabelSelector != "" || r.options.FieldSelector != "" {
		tweakListOptions = func(options *metav1.ListOptions) {
			if r.options.LabelSelector != "" {
				options.LabelSelector = r.options.LabelSelector
			}
			if r.options.FieldSelector != "" {
				options.FieldSelector = r.options.FieldSelector
			}
		}
	}

	// Define our informer TODO
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		r.dependencies.Client,
		r.options.InformerDurationToResync,
		namespace,
		tweakListOptions)

	r.rawInformer = factory.ForResource(r.options.GVR).Informer()
}

func (r *Informer) WithEventHandlerFunc(evHandlerFunc EventHandlerFunc) error {

	logger := log.FromContext(r.dependencies.Context).
		WithValues("gvr", r.options.GVR, "namespace", r.options.Namespace)

	//
	r.mu.RLock()
	if r.started {
		return fmt.Errorf("cannot add event handler after informer has started")
	}
	r.mu.RUnlock()

	// Register functions to handle different types of events
	handlers := cache.ResourceEventHandlerFuncs{

		//
		AddFunc: func(eventObject interface{}) {
			obj := eventObject.(*unstructured.Unstructured)

			logger = logger.WithValues(
				"eventType", EventTypeAdded,
				"object", obj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := evHandlerFunc(EventTypeAdded, obj, nil); err != nil {
				logger.Error(err, "Error in add reconciliation")
			}
		},

		//
		UpdateFunc: func(eventObjectOld, eventObject interface{}) {
			obj := eventObject.(*unstructured.Unstructured)
			oldObj := eventObjectOld.(*unstructured.Unstructured)

			logger = logger.WithValues(
				"eventType", EventTypeUpdated,
				"object", obj.GetName(),
				"oldObject", oldObj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := evHandlerFunc(EventTypeUpdated, obj, oldObj); err != nil {
				logger.Error(err, "Error in update reconciliation")
			}
		},

		//
		DeleteFunc: func(eventObject interface{}) {
			obj := eventObject.(*unstructured.Unstructured)

			logger = logger.WithValues(
				"eventType", EventTypeDeleted,
				"object", obj.GetName(),
			)
			logger.V(1).Info("Processing reconciliation")

			if err := evHandlerFunc(EventTypeDeleted, obj, nil); err != nil {
				logger.Error(err, "Error in delete reconciliation")
			}
		},
	}

	_, err := r.rawInformer.AddEventHandler(handlers)
	if err != nil {
		return fmt.Errorf("error adding handling functions for events to an informer: %s", err.Error())
	}

	return nil
}

func (r *Informer) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.rawInformer.IsStopped() {
		close(r.stopChan)
	}
}

func (r *Informer) Start() {
	//
	r.mu.Lock()
	r.started = true
	r.mu.Unlock()

	logger := log.FromContext(r.dependencies.Context).
		WithValues("gvr", r.options.GVR, "namespace", r.options.Namespace)

	// Listen to cancellation of parent context and propagate stopChan
	go func() {
		select {
		case <-r.dependencies.Context.Done():
			logger.Info(informerContextFinishedMessage)
			r.Stop()
		case <-r.stopChan:
			logger.Info(informerKilledMessage)
			return
		}
	}()

	//
	go func() {
		logger.Info(informerStartedMessage)
		r.rawInformer.Run(r.stopChan)
	}()
}

func (r *Informer) GetInformer() *cache.SharedIndexInformer {
	return &r.rawInformer
}
