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

// RecheckableStore is the minimal interface required by ConditionRecheckRunnable to manage
// per-policy recheck tickers. Any PolicyStore satisfies this interface automatically via
// its GetPolicyIntervals() and GetPolicyCollections() methods.
type RecheckableStore interface {
	// GetPolicyIntervals returns a map from policy name to its ConditionRecheckInterval,
	// for every policy with a non-zero interval.
	GetPolicyIntervals() map[string]time.Duration

	// GetPolicyCollections returns a map from policy name to the list of collection keys
	// (e.g. GVRNN keys) where that policy is registered. Used to build the set of
	// resource keys to re-evaluate on each recheck tick.
	GetPolicyCollections() map[string][]string
}

// RecheckEntry pairs a RecheckableStore with the ProcessFn that should be invoked on each
// recheck tick. Add one entry per policy kind that supports conditionRecheckInterval.
type RecheckEntry struct {
	// Store is the policy store to scan for policies with a non-zero ConditionRecheckInterval.
	Store RecheckableStore

	// ProcessFn is the processor function to call with a synthetic Modified event on each tick.
	ProcessFn func(resourceType string, eventType watch.EventType, objects ...map[string]interface{})
}

// recheckTickerState holds the runtime state of a single per-policy recheck goroutine.
type recheckTickerState struct {
	cancel   context.CancelFunc
	interval time.Duration
}

// ConditionRecheckRunnable is a leader-elected manager.Runnable that periodically re-evaluates
// policy conditions even when no watched-resource event has been received. It is generic:
// any policy kind can participate by providing a RecheckEntry.
//
// For each policy that has a non-zero ConditionRecheckInterval, it maintains an independent
// ticker goroutine. On every tick it fetches all objects currently in the pool for each of the
// policy's watched GVRNN keys and fires the ProcessFn with a synthetic Modified event.
// Ticker goroutines are started, stopped, or restarted as policies are added, updated, or removed.
// If a policy's ConditionRecheckInterval changes, its goroutine is restarted with the new period.
type ConditionRecheckRunnable struct {
	registry *informerRegistry.Registry
	entries  []RecheckEntry
	ctx      *context.Context

	mu      sync.Mutex
	tickers map[string]recheckTickerState // keyed by "{entryIndex}:{policyName}"
}

// NewConditionRecheckRunnable creates a ConditionRecheckRunnable. Parameters:
//   - registry: used to read the watched-object pool for synthetic events.
//   - entries: one RecheckEntry per policy kind that supports conditionRecheckInterval.
//   - ctx: application context pointer (same pattern as InformerManager).
func NewConditionRecheckRunnable(
	registry *informerRegistry.Registry,
	entries []RecheckEntry,
	ctx *context.Context,
) *ConditionRecheckRunnable {
	return &ConditionRecheckRunnable{
		registry: registry,
		entries:  entries,
		ctx:      ctx,
		tickers:  make(map[string]recheckTickerState),
	}
}

// NeedLeaderElection returns true — recheck processing must only happen on the leader
// to avoid duplicate generation/cleanup actions.
func (r *ConditionRecheckRunnable) NeedLeaderElection() bool { return true }

// Start runs the ConditionRecheckRunnable. It periodically reconciles the set of active
// per-policy recheck goroutines and blocks until the context is cancelled.
func (r *ConditionRecheckRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(*r.ctx).WithValues("controller", "condition-recheck")
	logger.Info("Starting Controller")

	ticker := time.NewTicker(secondsToReconcileInformers)
	defer ticker.Stop()

	r.reconcileRecheckTickers()
	for {
		select {
		case <-(*r.ctx).Done():
			r.stopAllTickers()
			logger.Info("Controller finished by context")
			return nil
		case <-ticker.C:
			r.reconcileRecheckTickers()
		}
	}
}

// reconcileRecheckTickers ensures exactly one recheck goroutine per (entry, policy) pair that
// has a non-zero ConditionRecheckInterval. Stale goroutines are cancelled; new ones are started;
// goroutines whose interval changed are restarted with the new period.
func (r *ConditionRecheckRunnable) reconcileRecheckTickers() {
	desired := r.collectDesiredTickers()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Stop tickers no longer desired or whose interval changed.
	for tickerKey, state := range r.tickers {
		d, ok := desired[tickerKey]
		if !ok || state.interval != d.interval {
			state.cancel()
			delete(r.tickers, tickerKey)
		}
	}

	// Start tickers for new or restarted entries.
	for tickerKey, spec := range desired {
		if _, running := r.tickers[tickerKey]; !running {
			tickCtx, cancel := context.WithCancel(*r.ctx)
			r.tickers[tickerKey] = recheckTickerState{cancel: cancel, interval: spec.interval}
			go r.recheckLoop(tickCtx, tickerKey, spec.interval, spec.collectionKeys, spec.processFn)
		}
	}
}

// desiredTickerSpec is transient state used during reconcileRecheckTickers.
type desiredTickerSpec struct {
	interval       time.Duration
	collectionKeys []string
	processFn      func(resourceType string, eventType watch.EventType, objects ...map[string]interface{})
}

// collectDesiredTickers returns a map from ticker key → desiredTickerSpec for every
// (entry, policy) pair that has a non-zero ConditionRecheckInterval.
// The ticker key is "{entryIndex}:{policyName}" to avoid collisions across entries.
func (r *ConditionRecheckRunnable) collectDesiredTickers() map[string]desiredTickerSpec {
	desired := make(map[string]desiredTickerSpec)

	for i, entry := range r.entries {
		intervals := entry.Store.GetPolicyIntervals()
		if len(intervals) == 0 {
			continue
		}
		collections := entry.Store.GetPolicyCollections()

		for policyName, interval := range intervals {
			tickerKey := fmt.Sprintf("%d:%s", i, policyName)
			desired[tickerKey] = desiredTickerSpec{
				interval:       interval,
				collectionKeys: collections[policyName],
				processFn:      entry.ProcessFn,
			}
		}
	}

	return desired
}

// recheckLoop is the per-(entry, policy) goroutine. On every tick it reads all objects for each
// of the collection keys from the registry pool and calls processFn with a synthetic Modified
// event, triggering a full condition re-evaluation.
func (r *ConditionRecheckRunnable) recheckLoop(
	ctx context.Context,
	tickerKey string,
	interval time.Duration,
	collectionKeys []string,
	processFn func(resourceType string, eventType watch.EventType, objects ...map[string]interface{}),
) {
	logger := log.FromContext(*r.ctx).WithValues("controller", "condition-recheck", "ticker", tickerKey)
	logger.Info("Starting recheck loop", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Recheck loop stopped")
			return
		case <-ticker.C:
			for _, key := range collectionKeys {
				for _, obj := range r.registry.GetPool(key) {
					go processFn(key, watch.Modified, *obj)
				}
			}
		}
	}
}

// stopAllTickers cancels every active recheck goroutine. Called on shutdown.
func (r *ConditionRecheckRunnable) stopAllTickers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, state := range r.tickers {
		state.cancel()
		delete(r.tickers, key)
	}
}
