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
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/globals"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"

	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
)

const (
	secondsToCheckInformerAck   = 10 * time.Second
	secondsToReconcileInformers = 2 * time.Second
	secondsToCleanInformers     = 5 * time.Second

	// Consumer name prefixes. Full consumer names follow the pattern:
	//   "{prefix}:{policyKind}:{policyName}"
	// Examples:
	//   "sources:ClusterGenerationPolicy:gen-labels"
	//   "watched:ClusterCleanPolicy:clean-orphans"
	consumerPrefixSources = "sources"
	consumerPrefixWatched = "watched"

	informerStartedMessage = "Informer for '%s' has been started"
	informerKilledMessage  = "Informer for resource type '%s' killed by StopSignal"
	informerLaunchError    = "Impossible to start informer for resource type: %s"
	gvrParsingError        = "Failed to parse key from resourceType"
)

// Options holds configuration for the InformerManager.
type Options struct {
	// InformerDurationToResync is the interval between full resync cycles
	// for all dynamic informers managed by this InformerManager.
	InformerDurationToResync time.Duration
}

// Dependencies holds the external registries that the InformerManager reads
// to determine which informers need to be running.
type Dependencies struct {
	Context *context.Context

	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	ClusterCleanPolicyRegistry      *policyStore.PolicyStore[*v1alpha1.ClusterCleanPolicy]
	ClusterClonePolicyRegistry      *policyStore.PolicyStore[*v1alpha1.ClusterClonePolicy]
	ClusterMutationPolicyRegistry   *policyStore.PolicyStore[*v1alpha1.ClusterMutationPolicy]
	ClusterValidationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterValidationPolicy]
}

// InformerManager unifies what were separate SourcesController and ObservedResourceController.
// It exposes two manager.Runnable implementations:
//   - SourcesRunnable: runs on ALL replicas (NeedLeaderElection=false), reconciles source informers
//     that cache Kubernetes objects for policy evaluation (admission webhooks need the pool).
//   - WatchedRunnable: runs on LEADER ONLY (NeedLeaderElection=true), reconciles watched-resource
//     informers that trigger generation and clean processors.
//
// Both runnables share the same Registry instance. Consumer names follow the uniform pattern
// "{prefix}:{policyKind}:{policyName}" so the refcount is exact per-policy.
type InformerManager struct {
	Registry *informerRegistry.Registry
	Options  Options
	Deps     Dependencies
}

// New creates an InformerManager wired to the given registry, options, and dependencies.
func New(registry *informerRegistry.Registry, opts Options, deps Dependencies) *InformerManager {
	return &InformerManager{
		Registry: registry,
		Options:  opts,
		Deps:     deps,
	}
}

// sourcesRunnable is a manager.Runnable that reconciles source informers on all replicas.
type sourcesRunnable struct {
	mgr *InformerManager
}

// NeedLeaderElection returns false — source informers must run on every replica
// because the admission webhook server needs the cached pool on each pod.
func (r *sourcesRunnable) NeedLeaderElection() bool { return false }

// Start launches the source informer reconciliation loop and cleaner worker.
// Blocks until the context is cancelled.
func (r *sourcesRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(*r.mgr.Deps.Context).WithValues("controller", "sources")
	logger.Info("Starting Controller")

	go r.mgr.cleanerWorker(consumerPrefixSources, "sources")

	ticker := time.NewTicker(secondsToReconcileInformers)
	defer ticker.Stop()

	r.mgr.reconcileSources()
	for {
		select {
		case <-(*r.mgr.Deps.Context).Done():
			logger.Info("Controller finished by context")
			return nil
		case <-ticker.C:
			r.mgr.reconcileSources()
		}
	}
}

// watchedRunnable is a manager.Runnable that reconciles watched-resource informers
// on the elected leader only.
type watchedRunnable struct {
	mgr *InformerManager
}

// NeedLeaderElection returns true — watched informers trigger generation and clean
// processing, which must only happen on one replica to avoid duplicate work.
func (r *watchedRunnable) NeedLeaderElection() bool { return true }

// Start launches the watched informer reconciliation loop and cleaner worker.
// Blocks until the context is cancelled.
func (r *watchedRunnable) Start(ctx context.Context) error {
	logger := log.FromContext(*r.mgr.Deps.Context).WithValues("controller", "watched")
	logger.Info("Starting Controller")

	go r.mgr.cleanerWorker(consumerPrefixWatched, "watched")

	ticker := time.NewTicker(secondsToReconcileInformers)
	defer ticker.Stop()

	r.mgr.reconcileWatched()
	for {
		select {
		case <-(*r.mgr.Deps.Context).Done():
			logger.Info("Controller finished by context")
			return nil
		case <-ticker.C:
			r.mgr.reconcileWatched()
		}
	}
}

// SourcesRunnable returns a manager.Runnable for source informer reconciliation.
func (m *InformerManager) SourcesRunnable() manager.Runnable {
	return &sourcesRunnable{mgr: m}
}

// WatchedRunnable returns a manager.Runnable for watched-resource informer reconciliation.
func (m *InformerManager) WatchedRunnable() manager.Runnable {
	return &watchedRunnable{mgr: m}
}

// policyKind constants used in consumer names.
const (
	kindClusterGenerationPolicy = "ClusterGenerationPolicy"
	kindClusterCleanPolicy      = "ClusterCleanPolicy"
	kindClusterClonePolicy      = "ClusterClonePolicy"
	kindClusterMutationPolicy   = "ClusterMutationPolicy"
	kindClusterValidationPolicy = "ClusterValidationPolicy"
)

// getSourceConsumers returns a map from GVR key to the set of consumer names
// that need an informer for that GVR. Each consumer follows the pattern
// "sources:{PolicyKind}:{PolicyName}".
//
// Example result:
//
//	{
//	  "/v1/configmaps": {
//	    "sources:ClusterGenerationPolicy:gen-labels":   true,
//	    "sources:ClusterMutationPolicy:inject-sidecar": true,
//	  },
//	}
func (m *InformerManager) getSourceConsumers() map[string]map[string]bool {
	result := make(map[string]map[string]bool)

	addFromStore := func(store interface{ GetReferencedSourcesByPolicy() map[string][]string }, kind string) {
		for gvrKey, policyNames := range store.GetReferencedSourcesByPolicy() {
			if result[gvrKey] == nil {
				result[gvrKey] = make(map[string]bool)
			}
			for _, name := range policyNames {
				result[gvrKey][consumerName(consumerPrefixSources, kind, name)] = true
			}
		}
	}

	addFromStore(m.Deps.ClusterGenerationPolicyRegistry, kindClusterGenerationPolicy)
	addFromStore(m.Deps.ClusterCleanPolicyRegistry, kindClusterCleanPolicy)
	addFromStore(m.Deps.ClusterMutationPolicyRegistry, kindClusterMutationPolicy)
	addFromStore(m.Deps.ClusterValidationPolicyRegistry, kindClusterValidationPolicy)

	return result
}

// getWatchedConsumers returns a map from GVRNN key to the set of consumer names
// that need a watched informer for that resource. Each consumer follows the pattern
// "watched:{PolicyKind}:{PolicyName}".
//
// Example result:
//
//	{
//	  "apps/v1/deployments/default/nginx": {
//	    "watched:ClusterGenerationPolicy:gen-labels":  true,
//	    "watched:ClusterCleanPolicy:clean-orphans":    true,
//	  },
//	}
func (m *InformerManager) getWatchedConsumers() map[string]map[string]bool {
	result := make(map[string]map[string]bool)

	addFromStore := func(store interface{ GetPolicyNamesByCollection() map[string][]string }, kind string) {
		for collKey, policyNames := range store.GetPolicyNamesByCollection() {
			if result[collKey] == nil {
				result[collKey] = make(map[string]bool)
			}
			for _, name := range policyNames {
				result[collKey][consumerName(consumerPrefixWatched, kind, name)] = true
			}
		}
	}

	addFromStore(m.Deps.ClusterGenerationPolicyRegistry, kindClusterGenerationPolicy)
	addFromStore(m.Deps.ClusterCleanPolicyRegistry, kindClusterCleanPolicy)
	addFromStore(m.Deps.ClusterClonePolicyRegistry, kindClusterClonePolicy)

	return result
}

// reconcileSources ensures that for every GVR referenced in any policy's 'sources' section,
// there is a running informer with all per-policy consumers registered.
func (m *InformerManager) reconcileSources() {
	logger := log.FromContext(*m.Deps.Context).WithValues("controller", "sources")

	desired := m.getSourceConsumers()
	for gvrKey, consumers := range desired {
		for consumer := range consumers {
			m.Registry.Register(gvrKey, consumer)
		}

		if m.Registry.IsStarted(gvrKey) {
			continue
		}

		go m.launchInformer(gvrKey, "sources")

		time.Sleep(secondsToCheckInformerAck)
		if !m.Registry.IsStarted(gvrKey) {
			logger.Info(fmt.Sprintf(informerLaunchError, gvrKey))
		}
	}
}

// reconcileWatched ensures that for every GVRNN referenced in generation/clean policies'
// 'watchedResources' section, there is a running informer with per-policy consumers.
func (m *InformerManager) reconcileWatched() {
	logger := log.FromContext(*m.Deps.Context).WithValues("controller", "watched")

	desired := m.getWatchedConsumers()
	for resourceKey, consumers := range desired {
		for consumer := range consumers {
			m.Registry.Register(resourceKey, consumer)
		}

		if m.Registry.IsStarted(resourceKey) {
			continue
		}

		go m.launchInformer(resourceKey, "watched")

		time.Sleep(secondsToCheckInformerAck)
		if !m.Registry.IsStarted(resourceKey) {
			logger.Info(fmt.Sprintf(informerLaunchError, resourceKey))
		}
	}
}

// cleanerWorker runs in the background and periodically removes consumers for policies
// that no longer exist, disabling informers when their refcount drops to zero.
func (m *InformerManager) cleanerWorker(prefix string, controllerName string) {
	logger := log.FromContext(*m.Deps.Context).WithValues("controller", controllerName)
	logger.Info("Starting Worker", "worker", "InformersCleaner")

	ticker := time.NewTicker(secondsToCleanInformers)
	defer ticker.Stop()

	for {
		select {
		case <-(*m.Deps.Context).Done():
			return
		case <-ticker.C:
			m.cleanStaleConsumers(prefix)
		}
	}
}

// cleanStaleConsumers compares the desired consumer set (from policy registries)
// with the actual consumers in the informer registry, removing any that are no
// longer needed and disabling informers whose refcount drops to zero.
func (m *InformerManager) cleanStaleConsumers(prefix string) {
	var desired map[string]map[string]bool
	if prefix == consumerPrefixSources {
		desired = m.getSourceConsumers()
	} else {
		desired = m.getWatchedConsumers()
	}

	for _, key := range m.Registry.GetRegisteredKeys() {
		desiredConsumers, keyStillNeeded := desired[key]

		if !keyStillNeeded {
			m.Registry.UnregisterByPrefix(key, prefix+":")
			if m.Registry.ConsumerCount(key) == 0 {
				m.Registry.Disable(key)
			}
			continue
		}

		m.removeStaleConsumersForKey(key, prefix, desiredConsumers)

		if m.Registry.ConsumerCount(key) == 0 {
			m.Registry.Disable(key)
		}
	}
}

// removeStaleConsumersForKey unregisters consumers with the given prefix that are
// registered for this key but not in the desired set.
func (m *InformerManager) removeStaleConsumersForKey(key string, prefix string, desiredConsumers map[string]bool) {
	entry, exists := m.Registry.GetEntry(key)
	if !exists {
		return
	}

	entry.Lock()
	var toRemove []string
	for consumer := range entry.GetConsumers() {
		if strings.HasPrefix(consumer, prefix+":") && !desiredConsumers[consumer] {
			toRemove = append(toRemove, consumer)
		}
	}
	entry.Unlock()

	for _, consumer := range toRemove {
		m.Registry.Unregister(key, consumer)
	}
}

// launchInformer creates and runs a Kubernetes dynamic informer for the given resource key.
// The key format determines how the informer is configured:
//   - 3-part (GVR: "{group}/{version}/{resource}"): watches all objects cluster-wide.
//   - 5-part (GVRNN: "{group}/{version}/{resource}/{namespace}/{name}"): optionally filters
//     by namespace and/or name.
//
// The informer runs until its StopSignal is triggered (via Registry.Disable) or the
// application context is cancelled.
func (m *InformerManager) launchInformer(resourceType string, controllerName string) {
	logger := log.FromContext(*m.Deps.Context).WithValues("controller", controllerName)

	entry, exists := m.Registry.GetEntry(resourceType)
	if !exists {
		m.Registry.Register(resourceType, controllerName+":bootstrap")
		entry, _ = m.Registry.GetEntry(resourceType)
		defer m.Registry.Unregister(resourceType, controllerName+":bootstrap")
	}

	logger.Info(fmt.Sprintf(informerStartedMessage, resourceType))

	m.Registry.SetStarted(resourceType, true)
	defer m.Registry.SetStarted(resourceType, false)

	parts := strings.Split(resourceType, "/")
	var resourceGVR schema.GroupVersionResource
	var namespace string
	var name string

	switch len(parts) {
	case 3:
		resourceGVR = schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: parts[2],
		}
		namespace = corev1.NamespaceAll
	case 5:
		resourceGVR = schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: parts[2],
		}
		namespace = corev1.NamespaceAll
		if parts[3] != "" {
			namespace = parts[3]
		}
		name = parts[4]
	default:
		logger.Info(gvrParsingError, "resourceType", resourceType)
		return
	}

	var listOptionsFunc dynamicinformer.TweakListOptionsFunc = func(options *metav1.ListOptions) {}
	if name != "" {
		listOptionsFunc = func(options *metav1.ListOptions) {
			options.FieldSelector = "metadata.name=" + name
		}
	}

	stopCh := make(chan struct{})
	go func() {
		<-entry.StopSignal
		close(stopCh)
		logger.Info(fmt.Sprintf(informerKilledMessage, resourceType))
	}()

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(globals.Application.KubeRawClient,
		m.Options.InformerDurationToResync, namespace, listOptionsFunc)

	kubeInformer := factory.ForResource(resourceGVR).Informer()

	// synced is set to true once the informer's initial list (cache sync) is complete.
	// AddFunc events fired before synced is true are pre-existing objects from the initial
	// list — they populate the sources pool but must not be dispatched to watched processors
	// to avoid flooding them with all existing objects on every controller restart.
	var synced atomic.Bool

	handlers := cache.ResourceEventHandlerFuncs{
		AddFunc: func(eventObject interface{}) {
			obj, err := common.UnstructuredFromInformerEvent(eventObject)
			if err != nil {
				logger.Error(err, "unexpected event object type in AddFunc")
				return
			}
			content := obj.UnstructuredContent()
			if synced.Load() {
				// Real new resource: broadcast to all listeners (pool + watched processors).
				m.Registry.Broadcast(resourceType, watch.Added, content)
			} else {
				// Sync initial: only update the pool, skip watched processors.
				m.Registry.AddToPool(resourceType, &content)
			}
		},
		UpdateFunc: func(eventObjectOld, eventObject interface{}) {
			oldObj, err := common.UnstructuredFromInformerEvent(eventObjectOld)
			if err != nil {
				logger.Error(err, "unexpected event object type in UpdateFunc (old)")
				return
			}
			newObj, err := common.UnstructuredFromInformerEvent(eventObject)
			if err != nil {
				logger.Error(err, "unexpected event object type in UpdateFunc")
				return
			}
			m.Registry.Broadcast(resourceType, watch.Modified,
				newObj.UnstructuredContent(), oldObj.UnstructuredContent())
		},
		DeleteFunc: func(eventObject interface{}) {
			obj, err := common.UnstructuredFromInformerEvent(eventObject)
			if err != nil {
				logger.Error(err, "unexpected event object type in DeleteFunc")
				return
			}
			m.Registry.Broadcast(resourceType, watch.Deleted, obj.UnstructuredContent())
		},
	}

	if _, err := kubeInformer.AddEventHandler(handlers); err != nil {
		logger.Error(err, "Error adding handling functions for events to an informer")
		return
	}

	// Start the informer in a goroutine so we can wait for cache sync on this goroutine.
	go kubeInformer.Run(stopCh)

	// Block until the initial list (cache sync) is complete, then mark synced so that
	// subsequent AddFunc calls are treated as real new-resource events.
	if cache.WaitForCacheSync(stopCh, kubeInformer.HasSynced) {
		synced.Store(true)
	}

	// Keep this goroutine alive until the informer stops.
	<-stopCh
}

// consumerName builds a consumer identifier following the canonical pattern
// "{prefix}:{policyKind}:{policyName}".
func consumerName(prefix, policyKind, policyName string) string {
	return prefix + ":" + policyKind + ":" + policyName
}
