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

	//
	"golang.org/x/exp/maps"

	"github.com/freepik-company/admitik/internal/keys"
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

	policies := s.collections[collectionName]
	index := -1
	for itemIndex, itemObject := range policies {
		if itemObject.GetName() == policy.GetName() {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		s.collections[collectionName] = append(policies[:index], policies[index+1:]...)
	}

	// Delete resource type from registry when no more policy resources need it
	if len(s.collections[collectionName]) == 0 {
		delete(s.collections, collectionName)
	}
}

// GetResources return all the policy objects of desired collection
func (s *PolicyStore[T]) GetResources(collectionName string) []T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list, ok := s.collections[collectionName]
	if !ok {
		return []T{}
	}

	result := make([]T, len(list))
	copy(result, list)
	return result
}

// GetCollectionNames returns a list of collection names
// collections are commonly named following pattern: {group}/{version}/{resource}/{operation}
func (s *PolicyStore[T]) GetCollectionNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return maps.Keys(s.collections)
}

// GetReferencedSources returns a deduplicated list of GVR keys referenced in the 'sources'
// section across all policies in all collections.
// GVR is expressed as {group}/{version}/{resource}.
func (s *PolicyStore[T]) GetReferencedSources() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sourceTypes := []string{}

	for _, collectionObjectList := range s.collections {
		for _, resourceObj := range collectionObjectList {
			for _, source := range resourceObj.GetSources() {
				if reflect.ValueOf(source).IsZero() {
					continue
				}
				sourceName := keys.GVRKey(source.Group, source.Version, source.Resource)
				sourceTypes = append(sourceTypes, sourceName)
			}
		}
	}

	slices.Sort(sourceTypes)
	sourceTypes = slices.Compact(sourceTypes)

	return sourceTypes
}

// GetReferencedSourcesByPolicy returns a map from GVR key to the list of policy names
// that reference it as a source. This enables per-policy refcounting in the informer registry.
//
// Example result:
//
//	{
//	  "/v1/configmaps":     ["gen-labels", "inject-sidecar"],
//	  "apps/v1/deployments": ["gen-annotations"],
//	}
func (s *PolicyStore[T]) GetReferencedSourcesByPolicy() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]string)

	for _, collectionObjectList := range s.collections {
		for _, resourceObj := range collectionObjectList {
			policyName := resourceObj.GetName()
			for _, source := range resourceObj.GetSources() {
				if reflect.ValueOf(source).IsZero() {
					continue
				}
				gvrKey := keys.GVRKey(source.Group, source.Version, source.Resource)
				if !slices.Contains(result[gvrKey], policyName) {
					result[gvrKey] = append(result[gvrKey], policyName)
				}
			}
		}
	}

	return result
}

// GetPolicyNamesByCollection returns a map from collection key (e.g. GVRNN) to the list
// of policy names stored in that collection. This enables per-policy refcounting
// in the informer registry for watched resources.
//
// Example result:
//
//	{
//	  "apps/v1/deployments/default/nginx": ["gen-labels", "clean-orphans"],
//	}
func (s *PolicyStore[T]) GetPolicyNamesByCollection() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]string)

	for collectionName, policies := range s.collections {
		for _, p := range policies {
			result[collectionName] = append(result[collectionName], p.GetName())
		}
	}

	return result
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
