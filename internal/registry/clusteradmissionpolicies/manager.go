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

package clusteradmissionpolicies

import (
	"freepik.com/admitik/api/v1alpha1"
	"golang.org/x/exp/maps"
	"reflect"
	"slices"
	"strings"
)

func NewClusterAdmissionPoliciesRegistry() *ClusterAdmissionPoliciesRegistry {
	return &ClusterAdmissionPoliciesRegistry{
		registry: make(map[ResourceTypeName][]*v1alpha1.ClusterAdmissionPolicy),
	}
}

// AddResource add a ClusterAdmissionPolicy of provided type into registry
func (m *ClusterAdmissionPoliciesRegistry) AddResource(rt ResourceTypeName, clusterAdmissionPolicy *v1alpha1.ClusterAdmissionPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.registry[rt] = append(m.registry[rt], clusterAdmissionPolicy)
}

// RemoveResource delete a RemoveClusterAdmissionPolicy of provided type
func (m *ClusterAdmissionPoliciesRegistry) RemoveResource(rt ResourceTypeName, clusterAdmissionPolicy *v1alpha1.ClusterAdmissionPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clusterAdmissionPolicies := m.registry[rt]
	index := -1
	for itemIndex, itemObject := range clusterAdmissionPolicies {
		if itemObject.Name == clusterAdmissionPolicy.Name {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		m.registry[rt] = append(clusterAdmissionPolicies[:index], clusterAdmissionPolicies[index+1:]...)
	}

	// Delete index from registry when any Notification resource is needing it
	if len(m.registry[rt]) == 0 {
		delete(m.registry, rt)
	}
}

// GetResources return all the ClusterAdmissionPolicy objects of provided type
func (m *ClusterAdmissionPoliciesRegistry) GetResources(rt ResourceTypeName) []*v1alpha1.ClusterAdmissionPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	if list, listFound := m.registry[rt]; listFound {
		return list
	}

	return []*v1alpha1.ClusterAdmissionPolicy{}
}

// GetRegisteredResourceTypes returns TODO
// FIXME: Is this still needed?
func (m *ClusterAdmissionPoliciesRegistry) GetRegisteredResourceTypes() []ResourceTypeName {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Keys(m.registry)
}

// GetRegisteredSourcesTypes returns TODO
func (m *ClusterAdmissionPoliciesRegistry) GetRegisteredSourcesTypes() []ResourceTypeName {

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
