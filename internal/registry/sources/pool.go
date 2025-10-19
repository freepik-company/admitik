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
	"golang.org/x/exp/maps"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GetRegisteredResourceTypes returns TODO
func (m *SourcesRegistry) GetRegisteredResourceTypes() []schema.GroupVersionResource {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Keys(m.informers)
}

// GetResources return all the objects of provided type
// Returning pointers to increase performance during templating stage with huge lists
func (m *SourcesRegistry) GetResources(gvr schema.GroupVersionResource) (results []*map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	informer, informerFound := m.informers[gvr]

	if !informerFound || informer == nil {
		return []*map[string]any{}
	}

	pointerList := m.informers[gvr].GetStore().List()

	for _, item := range pointerList {
		obj := item.(*unstructured.Unstructured)
		results = append(results, &obj.Object)
	}

	return results
}
