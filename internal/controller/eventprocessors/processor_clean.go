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

package eventprocessors

import (
	"k8s.io/apimachinery/pkg/watch"

	"github.com/freepik-company/admitik/api/v1alpha1"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
)

// CleanProcessorDependencies holds the external dependencies required by CleanProcessor.
type CleanProcessorDependencies struct {
	ClusterCleanPolicyRegistry  *policyStore.PolicyStore[*v1alpha1.ClusterCleanPolicy]
	SourcesPool                 informerRegistry.SourcesPool
	KubeAvailableResourceListFn func() []GVKR
}

// CleanProcessor handles events for watched resources and deletes target resources
// when conditions are met, according to each matching ClusterCleanPolicy.
//
// NOTE: This processor is a skeleton. The clean logic will be implemented in a
// future iteration once the ClusterCleanPolicy spec is redesigned.
type CleanProcessor struct {
	dependencies CleanProcessorDependencies
}

// NewCleanProcessor creates a CleanProcessor wired to the given dependencies.
func NewCleanProcessor(deps CleanProcessorDependencies) *CleanProcessor {
	return &CleanProcessor{dependencies: deps}
}

// Process is the entry point called by WatchedEventListener when a watched resource event arrives.
// NOTE: This is a no-op skeleton. The clean logic will be implemented in a future iteration.
func (p *CleanProcessor) Process(resourceType string, eventType watch.EventType, objects ...map[string]interface{}) {
}
