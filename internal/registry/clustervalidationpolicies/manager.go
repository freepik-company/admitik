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

package clustervalidationpolicies

import (
	"reflect"
	"slices"
	"strings"

	//
	"golang.org/x/exp/maps"

	//
	"freepik.com/admitik/api/v1alpha1"
)

func NewClusterValidationPoliciesRegistry() *ClusterValidationPoliciesRegistry {
	return &ClusterValidationPoliciesRegistry{
		registry: make(map[ResourceTypeName][]*v1alpha1.ClusterValidationPolicy),
	}
}

// AddResource add a ClusterValidationPolicy of provided type into registry
func (m *ClusterValidationPoliciesRegistry) AddResource(rt ResourceTypeName, clusterValidationPolicy *v1alpha1.ClusterValidationPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.registry[rt] = append(m.registry[rt], clusterValidationPolicy)
}

// RemoveResource delete a ClusterValidationPolicy of provided type
func (m *ClusterValidationPoliciesRegistry) RemoveResource(rt ResourceTypeName, clusterValidationPolicy *v1alpha1.ClusterValidationPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clusterValidationPolicies := m.registry[rt]
	index := -1
	for itemIndex, itemObject := range clusterValidationPolicies {
		if itemObject.Name == clusterValidationPolicy.Name {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		m.registry[rt] = append(clusterValidationPolicies[:index], clusterValidationPolicies[index+1:]...)
	}

	// Delete index from registry when no more ClusterValidationPolicy resource is needing it
	if len(m.registry[rt]) == 0 {
		delete(m.registry, rt)
	}
}

// GetResources return all the ClusterValidationPolicy objects of provided type
func (m *ClusterValidationPoliciesRegistry) GetResources(rt ResourceTypeName) []*v1alpha1.ClusterValidationPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	if list, listFound := m.registry[rt]; listFound {
		return list
	}

	return []*v1alpha1.ClusterValidationPolicy{}
}

// GetRegisteredResourceTypes returns a list of resource groups that will be evaluated by the admissions server
func (m *ClusterValidationPoliciesRegistry) GetRegisteredResourceTypes() []ResourceTypeName {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Keys(m.registry)
}

// GetRegisteredSourcesTypes returns a list of resource groups that the user desires to watch for later
// injection in templates that will be evaluated by controllers
func (m *ClusterValidationPoliciesRegistry) GetRegisteredSourcesTypes() []ResourceTypeName {

	m.mu.Lock()
	defer m.mu.Unlock()

	sourceTypes := []ResourceTypeName{}

	// Loop over all notifications collecting extra resources
	for _, resourceList := range m.registry {
		for _, resourceObj := range resourceList {
			for _, source := range resourceObj.Spec.Sources {

				// Prevents potential explosions due to
				// 'sources' comes empty from time to time
				if reflect.ValueOf(source).IsZero() {
					continue
				}

				sourceName := strings.Join([]string{
					source.Group, source.Version, source.Resource,
					source.Namespace, source.Name,
				}, "/")

				sourceTypes = append(sourceTypes, sourceName)
			}
		}
	}

	// Clean duplicated
	slices.Sort(sourceTypes)
	sourceTypes = slices.Compact(sourceTypes)

	return sourceTypes
}
