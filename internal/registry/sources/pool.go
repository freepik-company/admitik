package sources

import (
	"freepik.com/admitik/internal/globals"
	"golang.org/x/exp/maps"
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
		if objectData["name"] == resourceData["name"] && objectData["namespace"] == resourceData["namespace"] {
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
