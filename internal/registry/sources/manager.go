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

package sources

import (
	"errors"
	"time"
)

// NewSourcesRegistry TODO
func NewSourcesRegistry() *SourcesRegistry {

	return &SourcesRegistry{
		informers: make(map[ResourceTypeName]*SourcesInformer),
	}
}

// RegisterInformer registers an informer for required resource type
func (m *SourcesRegistry) RegisterInformer(rt ResourceTypeName) *SourcesInformer {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.informers[rt] = &SourcesInformer{
		Started:    false,
		StopSignal: make(chan bool),
	}

	return m.informers[rt]
}

// DisableInformer send a signal to the informer to stop
// and delete it from the registry
func (m *SourcesRegistry) DisableInformer(rt ResourceTypeName) error {
	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("extra-resource informer not found")
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
		return errors.New("impossible to stop the extra-resource informer")
	}

	// Delete informer from registry
	m.mu.Lock()
	delete(m.informers, rt)
	m.mu.Unlock()

	return nil
}

// SetStarted updates the 'started' flag of an informer
func (m *SourcesRegistry) SetStarted(rt ResourceTypeName, started bool) error {
	informer, exists := m.GetInformer(rt)
	if !exists {
		return errors.New("extra-resource informer not found")
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	informer.Started = started
	return nil
}

// IsStarted returns whether an informer of the provided resource type is started or not
func (m *SourcesRegistry) IsStarted(rt ResourceTypeName) bool {
	informer, exists := m.GetInformer(rt)
	if !exists {
		return false
	}

	informer.mu.Lock()
	defer informer.mu.Unlock()

	return informer.Started
}

// GetInformer return the informer attached to a resource type
func (m *SourcesRegistry) GetInformer(rt ResourceTypeName) (informer *SourcesInformer, exists bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	//
	informer, exists = m.informers[rt]
	return informer, exists
}
