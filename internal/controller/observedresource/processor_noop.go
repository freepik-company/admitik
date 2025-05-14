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
	"freepik.com/admitik/internal/globals"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type NoopProcessorDependencies struct{}

type NoopProcessor struct {
	dependencies NoopProcessorDependencies
}

func NewNoopProcessor(deps NoopProcessorDependencies) *NoopProcessor {
	return &NoopProcessor{
		dependencies: deps,
	}
}

func (p *NoopProcessor) Process(resourceType string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context)
	logger = logger.WithValues("processor", ObserverTypeNoop)
}
