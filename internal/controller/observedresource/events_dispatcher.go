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
	"fmt"
	"time"

	//
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/globals"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	resourceObserverRegistry "github.com/freepik-company/admitik/internal/registry/resourceobserver"
	sourcesRegistry "github.com/freepik-company/admitik/internal/registry/sources"
)

const (
	ObserverTypeNoop                      = "noop"
	ObserverTypeClusterGenerationPolicies = "clustergenerationpolicies"
)

type EventDispatcherDependencies struct {
	//
	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	SourcesRegistry                 *sourcesRegistry.SourcesRegistry
	ResourceObserverRegistry        *resourceObserverRegistry.ResourceObserverRegistry

	//
}

type EventDispatcher struct {
	dependencies EventDispatcherDependencies

	// Carried stuff
	processors                map[string]Processor
	kubeAvailableResourceList []GVKR
}

// NewEventDispatcher TODO
func NewEventDispatcher(deps EventDispatcherDependencies) *EventDispatcher {

	evDispatcher := &EventDispatcher{
		dependencies: deps,
	}

	// Start syncer for available resources in Kubernetes
	evDispatcher.kubeAvailableResourceList = []GVKR{}
	go evDispatcher.syncKubeAvailableResources()

	//
	evDispatcher.processors = evDispatcher.getInitializedProcessors()

	return evDispatcher
}

// syncKubeAvailableResources TODO
// This function is intended to be used as goroutine
func (d *EventDispatcher) syncKubeAvailableResources() {
	logger := log.FromContext(globals.Application.Context).WithValues("controller", controllerName)

	logger.Info("Starting Worker", "worker", "KubeAvailableResourcesSyncer")

	for {
		resources, err := fetchKubeAvailableResources()

		if err != nil {
			logger.Info(fmt.Sprintf("Failed fetching Kubernetes available resources list: %v", err.Error()))
			goto takeANap
		}

		d.kubeAvailableResourceList = *resources

	takeANap:
		time.Sleep(5 * time.Second)
	}
}

// getInitializedProcessors return a map with all the processors indexed by type
func (d *EventDispatcher) getInitializedProcessors() (processorsMap map[string]Processor) {
	processors := map[string]Processor{}

	processors[ObserverTypeNoop] = NewNoopProcessor(NoopProcessorDependencies{})

	processors[ObserverTypeClusterGenerationPolicies] = NewGenerationProcessor(GenerationProcessorDependencies{
		ClusterGenerationPolicyRegistry: d.dependencies.ClusterGenerationPolicyRegistry,
		SourcesRegistry:                 d.dependencies.SourcesRegistry,
		KubeAvailableResourceList:       &d.kubeAvailableResourceList,
	})

	processorsMap = processors
	return processorsMap
}

// Dispatch TODO
func (d *EventDispatcher) Dispatch(resource string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context)
	_ = logger

	// Skip events when nobody is observing them
	obs := d.dependencies.ResourceObserverRegistry.GetObservers(resource)
	if len(obs) == 0 {
		return
	}

	// Trigger specific observer-related processors
	for _, o := range obs {
		if p, ok := d.processors[o]; ok {
			go p.Process(resource, eventType, object...)
		}
	}
}
