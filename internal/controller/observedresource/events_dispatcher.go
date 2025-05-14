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
	"freepik.com/admitik/internal/globals"
	//
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	clusterGenerationPoliciesRegistry "freepik.com/admitik/internal/registry/clustergenerationpolicies"
	resourceObserverRegistry "freepik.com/admitik/internal/registry/resourceobserver"
	sourcesRegistry "freepik.com/admitik/internal/registry/sources"
)

const (
	ObserverTypeNoop                      = "noop"
	ObserverTypeClusterGenerationPolicies = "clustergenerationpolicies"
)

type EventDispatcherDependencies struct {
	//
	ClusterGenerationPoliciesRegistry *clusterGenerationPoliciesRegistry.ClusterGenerationPoliciesRegistry
	SourcesRegistry                   *sourcesRegistry.SourcesRegistry
	ResourceObserverRegistry          *resourceObserverRegistry.ResourceObserverRegistry

	//
	KubeAvailableResourceList *[]GVKR
}

type EventDispatcher struct {
	dependencies EventDispatcherDependencies
	processors   map[string]Processor
}

// NewEventDispatcher TODO
func NewEventDispatcher(deps EventDispatcherDependencies) *EventDispatcher {

	evDispatcher := &EventDispatcher{
		dependencies: deps,
	}

	evDispatcher.processors = evDispatcher.getInitializedProcessors()

	return evDispatcher
}

// getInitializedProcessors return a map with all the processors indexed by type
func (d *EventDispatcher) getInitializedProcessors() (processorsMap map[string]Processor) {
	processors := map[string]Processor{}

	processors[ObserverTypeNoop] = NewNoopProcessor(NoopProcessorDependencies{})

	processors[ObserverTypeClusterGenerationPolicies] = NewGenerationProcessor(GenerationProcessorDependencies{
		ClusterGenerationPoliciesRegistry: d.dependencies.ClusterGenerationPoliciesRegistry,
		SourcesRegistry:                   d.dependencies.SourcesRegistry,
		KubeAvailableResourceList:         d.dependencies.KubeAvailableResourceList,
	})

	processorsMap = processors
	return processorsMap
}

// Dispatch TODO
func (d *EventDispatcher) Dispatch(resource string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context)

	obs, err := d.dependencies.ResourceObserverRegistry.GetObservers(resource)
	if err != nil {
		logger.Info(fmt.Sprintf("failed getting observers. Skipping processor launch: %v", err.Error()))
		return
	}

	for _, o := range obs {
		if p, ok := d.processors[o]; ok {
			go p.Process(resource, eventType, object...)
		}
	}
}
