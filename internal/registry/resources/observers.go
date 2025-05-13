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

package resources

import (
	"errors"
	"slices"
)

// AddObserver adds an observer to the specified resource type's informer
func (m *ResourcesRegistry) AddObserver(rt ResourceTypeName, observer string) error {

	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("informer not found")
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	informer.Observers = append(informer.Observers, observer)
	return nil
}

// DeleteObserver deletes an observer from the specified resource type's informer
func (m *ResourcesRegistry) DeleteObserver(rt ResourceTypeName, observer string) error {

	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("informer not found")
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	informer.Observers = slices.DeleteFunc(informer.Observers, func(itemUnderReview string) bool {
		return itemUnderReview == observer
	})

	return nil
}

// TODO
func (m *ResourcesRegistry) SetObservers(rt ResourceTypeName, observers []string) error {

	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("informer not found")
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	informer.Observers = observers

	return nil
}

// TODO
func (m *ResourcesRegistry) GetObservers(rt ResourceTypeName) ([]string, error) {

	informer, exists := m.GetInformer(rt)
	if !exists {
		return nil, errors.New("informer not found")
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	return informer.Observers, nil
}
