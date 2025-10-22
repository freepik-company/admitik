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

package policystore

import (
	"reflect"
	"slices"
	"strings"

	//
	"golang.org/x/exp/maps"
)

func NewPolicyStore[T PolicyResourceI]() *PolicyStore[T] {
	return &PolicyStore[T]{
		collections: make(map[string][]T),
	}
}

// AddOrUpdateResource add a policy to a collection.
// When the policy already exists, updates it
func (s *PolicyStore[T]) AddOrUpdateResource(collectionName string, policy T) {

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replace it when found
	policies := s.collections[collectionName]
	for policyIndex, policyObject := range policies {
		if policyObject.GetName() == policy.GetName() {
			s.collections[collectionName][policyIndex] = policy
			return
		}
	}

	// Create it when missing
	s.collections[collectionName] = append(s.collections[collectionName], policy)
}

// RemoveResource delete a policy from a collection
func (s *PolicyStore[T]) RemoveResource(collectionName string, policy T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clusterValidationPolicies := s.collections[collectionName]
	index := -1
	for itemIndex, itemObject := range clusterValidationPolicies {
		if itemObject.GetName() == policy.GetName() {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		s.collections[collectionName] = append(clusterValidationPolicies[:index], clusterValidationPolicies[index+1:]...)
	}

	// Delete resource type from registry when no more ClusterValidationPolicy resource is needing it
	if len(s.collections[collectionName]) == 0 {
		delete(s.collections, collectionName)
	}
}

// GetResources return all the policy objects of desired collection
func (s *PolicyStore[T]) GetResources(collectionName string) []T {
	s.mu.Lock()
	defer s.mu.Unlock()

	//
	if list, listFound := s.collections[collectionName]; listFound {
		return list
	}

	return []T{}
}

// GetCollectionNames returns a list of collection names
// collections are commonly named following pattern: {group}/{version}/{resource}/{operation}
func (s *PolicyStore[T]) GetCollectionNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return maps.Keys(s.collections)
}

// GetReferencedSources returns a list of GVR names referenced on 'sources' section across the policies.
// GVR is expressed as {group}/{version}/{resource}
func (s *PolicyStore[T]) GetReferencedSources() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	sourceTypes := []string{}

	// Loop over all objects collecting extra resources
	for _, collectionObjectList := range s.collections {

		for _, resourceObj := range collectionObjectList {

			for _, source := range resourceObj.GetSources() {

				// Prevents potential explosions due to
				// 'sources' comes empty from time to time
				if reflect.ValueOf(source).IsZero() {
					continue
				}

				sourceName := strings.Join([]string{source.Group, source.Version, source.Resource}, "/")
				sourceTypes = append(sourceTypes, sourceName)
			}
		}
	}

	// Clean duplicated
	slices.Sort(sourceTypes)
	sourceTypes = slices.Compact(sourceTypes)

	return sourceTypes
}

// SortCollection sorts using a custom comparison function
func (s *PolicyStore[T]) SortCollection(
	collectionName string,
	less func(a, b T) bool,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, found := s.collections[collectionName]
	if !found || len(list) == 0 {
		return
	}

	slices.SortFunc(list, func(a, b T) int {
		if less(a, b) {
			return -1
		}
		if less(b, a) {
			return 1
		}
		return 0
	})

	s.collections[collectionName] = list
}
