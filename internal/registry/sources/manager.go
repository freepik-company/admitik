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
	"github.com/freepik-company/admitik/internal/informer"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NewSourcesRegistry TODO
func NewSourcesRegistry() *SourcesRegistry {

	return &SourcesRegistry{
		informers: make(map[schema.GroupVersionResource]*informer.Informer),
	}
}

// InformerIsRegistered TODO
func (m *SourcesRegistry) InformerIsRegistered(gvr schema.GroupVersionResource) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, informerExists := m.informers[gvr]
	if !informerExists {
		return false
	}

	return true
}

// RegisterInformer registers an informer for required GVR.
// Registering the informer just creates a placeholder for it: actual creation and launching will be done
// by the controller using this registry
func (m *SourcesRegistry) RegisterInformer(gvr schema.GroupVersionResource, inf *informer.Informer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.informers[gvr] = inf
}

// DestroyInformer send a signal to the informer to stop
// and delete it from the registry
func (m *SourcesRegistry) DestroyInformer(gvr schema.GroupVersionResource) {
	m.mu.Lock()
	defer m.mu.Unlock()

	informerObject, informerExists := m.informers[gvr]
	if !informerExists {
		return
	}

	// Just being explicit, dude
	if informerExists && informerObject != nil {
		m.informers[gvr].Stop()
	}

	// Delete informer from registry
	delete(m.informers, gvr)
}

func (m *SourcesRegistry) GetInformer(gvr schema.GroupVersionResource) *informer.Informer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.InformerIsRegistered(gvr) {
		return nil
	}

	return m.informers[gvr]
}
