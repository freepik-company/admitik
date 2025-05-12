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

package watchedresources

import (
	"errors"
	"golang.org/x/exp/maps"
	"time"
)

// NewWatchedResourcesRegistry TODO
func NewWatchedResourcesRegistry() *WatchedResourcesRegistry {

	return &WatchedResourcesRegistry{
		informers: make(map[ResourceTypeName]*WatchedResourcesInformer),
	}
}

// RegisterInformer registers an informer for required resource type
func (m *WatchedResourcesRegistry) RegisterInformer(rt ResourceTypeName) *WatchedResourcesInformer {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.informers[rt] = &WatchedResourcesInformer{
		Started:    false,
		StopSignal: make(chan bool),
	}

	return m.informers[rt]
}

// DisableInformer send a signal to the informer to stop
// and delete it from the registry
func (m *WatchedResourcesRegistry) DisableInformer(rt ResourceTypeName) error {
	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("informer not found")
	}

	// Send a signal to stop the informer
	informer.mu.Lock()
	informer.StopSignal <- true
	informer.mu.Unlock()

	// Wait for some time to stop the informer
	stoppedInformer := false
	for i := 0; i < 10; i++ {
		if !m.IsStarted(rt) {
			stoppedInformer = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !stoppedInformer {
		return errors.New("impossible to stop the informer")
	}

	// Delete informer from registry
	m.mu.Lock()
	delete(m.informers, rt)
	m.mu.Unlock()

	return nil
}

// GetInformer return the informer attached to a resource type
func (m *WatchedResourcesRegistry) GetInformer(rt ResourceTypeName) (informer *WatchedResourcesInformer, exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	informer, exists = m.informers[rt]
	return informer, exists
}

// GetRegisteredResourceTypes returns TODO
func (m *WatchedResourcesRegistry) GetRegisteredResourceTypes() []ResourceTypeName {
	m.mu.Lock()
	defer m.mu.Unlock()

	return maps.Keys(m.informers)
}

// SetStarted updates the 'started' flag of an informer
func (m *WatchedResourcesRegistry) SetStarted(rt ResourceTypeName, started bool) error {
	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("informer not found")
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	informer.Started = started
	return nil
}

// IsStarted returns whether an informer of the provided resource type is started or not
func (m *WatchedResourcesRegistry) IsStarted(rt ResourceTypeName) bool {
	informer, exists := m.GetInformer(rt)
	if !exists {
		return false
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	return informer.Started
}
