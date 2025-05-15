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

package resourceobserver

// NewResourceObserverRegistry TODO
func NewResourceObserverRegistry() *ResourceObserverRegistry {

	return &ResourceObserverRegistry{
		observers: make(map[ResourceTypeName]*ResourceObserverGroup),
	}
}

// GetObserverGroup return the observer group attached to a resource type
func (m *ResourceObserverRegistry) getObserverGroup(rt ResourceTypeName) (og *ResourceObserverGroup, exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	og, exists = m.observers[rt]
	return og, exists
}

// createObserverGroup create an observer group attached to a resource type
func (m *ResourceObserverRegistry) createObserverGroup(rt ResourceTypeName) (og *ResourceObserverGroup) {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	og = &ResourceObserverGroup{}
	m.observers[rt] = og

	//
	return og
}

// SetObservers TODO
func (m *ResourceObserverRegistry) SetObservers(rt ResourceTypeName, observers []string) {

	og, exists := m.getObserverGroup(rt)
	if !exists {
		og = m.createObserverGroup(rt)
	}

	//
	og.mu.Lock()
	defer og.mu.Unlock()

	//
	og.observers = observers
}

// GetObservers TODO
func (m *ResourceObserverRegistry) GetObservers(rt ResourceTypeName) []string {

	og, exists := m.getObserverGroup(rt)
	if !exists {
		return []string{}
	}

	og.mu.Lock()
	defer og.mu.Unlock()

	return og.observers
}
