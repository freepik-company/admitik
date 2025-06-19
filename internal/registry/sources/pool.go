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

	//
	"github.com/freepik-company/admitik/internal/globals"
)

// GetResources return all the objects of provided type
// Returning pointers to increase performance during templating stage with huge lists
func (m *SourcesRegistry) GetResources(rt ResourceTypeName) (results []*map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	if _, informerFound := m.informers[rt]; !informerFound {
		return []*map[string]any{}
	}

	m.informers[rt].mu.Lock()
	defer m.informers[rt].mu.Unlock()

	return m.informers[rt].ItemPool
}

// AddResource add a resource of provided type into registry
func (m *SourcesRegistry) AddResource(rt ResourceTypeName, resource *map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.informers[rt].ItemPool = append(m.informers[rt].ItemPool, resource)
}

// RemoveResource delete a resource of provided type
func (m *SourcesRegistry) RemoveResource(rt ResourceTypeName, resource *map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	resourceData, err := globals.GetObjectBasicData(resource)
	if err != nil {
		return err
	}

	// Locate the item and delete it when found
	informer := m.informers[rt]
	index := -1
	for itemIndex, itemObject := range informer.ItemPool {

		objectData, err := globals.GetObjectBasicData(itemObject)
		if err != nil {
			return err
		}

		//
		if objectData.Name == resourceData.Name && objectData.Namespace == resourceData.Namespace {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		informer.ItemPool = append(informer.ItemPool[:index], informer.ItemPool[index+1:]...)
	}

	return nil
}

// GetRegisteredResourceTypes returns TODO
func (m *SourcesRegistry) GetRegisteredResourceTypes() []ResourceTypeName {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Keys(m.informers)
}
