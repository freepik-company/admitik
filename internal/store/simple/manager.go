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

package simple

import (
	//
	"golang.org/x/exp/maps"
)

func NewStore[T StoreResourceI]() *Store[T] {
	return &Store[T]{
		collections: make(map[string][]T),
	}
}

// AddOrUpdateResource add an object to a collection.
// When the object already exists, updates it
func (s *Store[T]) AddOrUpdateResource(collectionName string, object T) {

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replace it when found
	tmpCollection := s.collections[collectionName]
	for itemIndex, itemObject := range tmpCollection {
		if itemObject.GetUniqueIdentifier() == object.GetUniqueIdentifier() {
			s.collections[collectionName][itemIndex] = object
			return
		}
	}

	// Create it when missing
	s.collections[collectionName] = append(s.collections[collectionName], object)
}

// RemoveResource delete an object from a collection
func (s *Store[T]) RemoveResource(collectionName string, object T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tmpCollection := s.collections[collectionName]
	index := -1
	for itemIndex, itemObject := range tmpCollection {
		if itemObject.GetUniqueIdentifier() == object.GetUniqueIdentifier() {
			index = itemIndex
			break
		}
	}
	if index != -1 {
		s.collections[collectionName] = append(tmpCollection[:index], tmpCollection[index+1:]...)
	}

	// Delete the collection when no more objects existing inside it
	if len(s.collections[collectionName]) == 0 {
		delete(s.collections, collectionName)
	}
}

// GetResources return all the objects of desired collection
func (s *Store[T]) GetResources(collectionName string) []T {
	s.mu.RLock()
	defer s.mu.RUnlock()

	//
	if list, listFound := s.collections[collectionName]; listFound {
		return list
	}

	return []T{}
}

// GetCollectionNames returns a list of collection names
// collections are commonly named following pattern: {group}/{version}/{resource}/{operation}
func (s *Store[T]) GetCollectionNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return maps.Keys(s.collections)
}
