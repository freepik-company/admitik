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

package clustermutationpolicy

import (
	"reflect"
	"slices"
	"strings"

	//
	"golang.org/x/exp/maps"

	//
	"freepik.com/admitik/api/v1alpha1"
)

func NewClusterMutationPolicyRegistry() *ClusterMutationPolicyRegistry {
	return &ClusterMutationPolicyRegistry{
		registry: make(map[ResourceTypeName][]*v1alpha1.ClusterMutationPolicy),
	}
}

// AddResource add a ClusterMutationPolicy of provided type into registry
func (m *ClusterMutationPolicyRegistry) AddResource(rt ResourceTypeName, clusterMutationPolicy *v1alpha1.ClusterMutationPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.registry[rt] = append(m.registry[rt], clusterMutationPolicy)
}

// RemoveResource delete a ClusterMutationPolicy of provided type
func (m *ClusterMutationPolicyRegistry) RemoveResource(rt ResourceTypeName, clusterMutationPolicy *v1alpha1.ClusterMutationPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clusterMutationPolicies := m.registry[rt]
	index := -1
	for itemIndex, itemObject := range clusterMutationPolicies {
		if itemObject.Name == clusterMutationPolicy.Name {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		m.registry[rt] = append(clusterMutationPolicies[:index], clusterMutationPolicies[index+1:]...)
	}

	// Delete index from registry when no more ClusterMutationPolicy resource is needing it
	if len(m.registry[rt]) == 0 {
		delete(m.registry, rt)
	}
}

// GetResources return all the ClusterMutationPolicy objects of provided type
func (m *ClusterMutationPolicyRegistry) GetResources(rt ResourceTypeName) []*v1alpha1.ClusterMutationPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	if list, listFound := m.registry[rt]; listFound {
		return list
	}

	return []*v1alpha1.ClusterMutationPolicy{}
}

// GetRegisteredResourceTypes returns a list of resource groups that will be evaluated by the mutations server
func (m *ClusterMutationPolicyRegistry) GetRegisteredResourceTypes() []ResourceTypeName {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Keys(m.registry)
}

// GetRegisteredSourcesTypes returns a list of resource groups that the user desires to watch for later
// injection in templates that will be evaluated by controllers
func (m *ClusterMutationPolicyRegistry) GetRegisteredSourcesTypes() []ResourceTypeName {

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
