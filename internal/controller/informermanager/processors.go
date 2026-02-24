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

package informermanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/globals"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
)

// GVKR is an alias for common.GVKR — a GroupVersionKind plus Resource/Subresource metadata.
// Used to map GVK→resource name when creating Kubernetes objects.
type GVKR = common.GVKR

// fetchKubeAvailableResources queries the Kubernetes discovery API to get all available
// API resources as GVKR entries. Used by KubeResourceSyncer to maintain a cached list
// for GVK-to-resource-name resolution during generation and clean processing.
func fetchKubeAvailableResources() (*[]GVKR, error) {
	_, apiResourceLists, err := globals.Application.KubeDiscoveryClient.ServerGroupsAndResources()
	if err != nil {
		return nil, err
	}

	var resources []GVKR
	for _, list := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range list.APIResources {
			resource := r.Name
			subresource := ""
			if idx := strings.Index(r.Name, "/"); idx != -1 {
				resource = r.Name[:idx]
				subresource = r.Name[idx+1:]
			}
			resources = append(resources, GVKR{
				GVK: schema.GroupVersionKind{
					Group:   gv.Group,
					Version: gv.Version,
					Kind:    r.Kind,
				},
				Resource:    resource,
				Subresource: subresource,
				Namespaced:  r.Namespaced,
			})
		}
	}

	return &resources, nil
}

// KubeResourceSyncer periodically fetches the list of available Kubernetes API resources
// via the discovery client and caches them for thread-safe access.
// Used by generation and clean processors to resolve GVK → resource name.
type KubeResourceSyncer struct {
	mu        sync.RWMutex
	resources []GVKR
	ctx       context.Context
}

// NewKubeResourceSyncer creates a syncer that immediately starts a background loop
// refreshing the available API resources every 5 seconds.
func NewKubeResourceSyncer(ctx context.Context) *KubeResourceSyncer {
	s := &KubeResourceSyncer{
		ctx:       ctx,
		resources: []GVKR{},
	}
	go s.syncLoop()
	return s
}

// syncLoop runs until the context is cancelled, refreshing the resource list every 5 seconds.
func (s *KubeResourceSyncer) syncLoop() {
	logger := log.FromContext(s.ctx).WithValues("controller", "watched")
	logger.Info("Starting Worker", "worker", "KubeAvailableResourcesSyncer")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			resources, err := fetchKubeAvailableResources()
			if err != nil {
				logger.Info(fmt.Sprintf("Failed fetching Kubernetes available resources list: %v", err.Error()))
			} else {
				s.mu.Lock()
				s.resources = *resources
				s.mu.Unlock()
			}
		}
	}
}

// GetResources returns a thread-safe copy of the cached API resources list.
func (s *KubeResourceSyncer) GetResources() []GVKR {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]GVKR, len(s.resources))
	copy(result, s.resources)
	return result
}

// WatchedProcessorEntry maps a policy kind to the function that processes events for that kind.
type WatchedProcessorEntry struct {
	// PolicyKind is the CRD kind name used in consumer naming (e.g. "ClusterGenerationPolicy").
	PolicyKind string

	// ProcessFn is the processor's Process method that handles resource events.
	ProcessFn func(resourceType string, eventType watch.EventType, objects ...map[string]interface{})
}

// WatchedEventListener is an EventListener that routes informer events to the appropriate
// generation/clean processors. For each registered processor, it checks whether the informer
// key has any consumer with the prefix "watched:{PolicyKind}:" — if so, the processor is
// invoked in a separate goroutine.
//
// This replaces the old EventDispatcher + ResourceObserverRegistry combination.
type WatchedEventListener struct {
	registry   *informerRegistry.Registry
	processors []WatchedProcessorEntry
}

// NewWatchedEventListener creates a WatchedEventListener with the given processor entries.
// Each entry maps a policy kind to a Process function.
func NewWatchedEventListener(
	registry *informerRegistry.Registry,
	processors []WatchedProcessorEntry,
) *WatchedEventListener {
	return &WatchedEventListener{
		registry:   registry,
		processors: processors,
	}
}

// OnEvent implements EventListener. For each registered processor, it checks if any
// consumer with the matching "watched:{PolicyKind}:" prefix is registered for this key.
// If so, the processor is invoked asynchronously.
func (w *WatchedEventListener) OnEvent(resourceKey informerRegistry.ResourceKey, eventType watch.EventType, objects ...map[string]interface{}) {
	for _, entry := range w.processors {
		prefix := consumerPrefixWatched + ":" + entry.PolicyKind + ":"
		if w.registry.HasAnyConsumerWithPrefix(resourceKey, prefix) {
			go entry.ProcessFn(resourceKey, eventType, objects...)
		}
	}
}
