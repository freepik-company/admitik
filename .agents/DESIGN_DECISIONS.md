# Design Decisions — Informer Registry & Controller Unification

This document records the key design decisions made during the Phase 4 refactor
(registry and controller unification). It is intended as a reference for future
contributors working on this codebase.

---

## 1. Why a single unified Registry?

### Problem

Before this refactor, there were **three separate registries** and **two controllers** managing
dynamic informers:

| Component | Purpose |
|-----------|---------|
| `ResourceInformerRegistry` | Informer lifecycle (start/stop/started flag) for watched resources |
| `SourcesRegistry` | Same lifecycle + an object pool (`[]*map[string]any`) for source caching |
| `ResourceObserverRegistry` | Routing table: which processor types want events for which informer |
| `ObservedResourceController` | Reconcile loop for watched informers (leader-elected) |
| `SourcesController` | Reconcile loop for source informers (all replicas) |

`ResourceInformerRegistry` ≈ `SourcesRegistry` (the lifecycle half) was near-identical
duplicated code. The observer registry was just a separate map of `[]string` observers.
Two controllers had ~200 lines each of the same reconcile/cleaner/launch pattern.

### Decision

Replace all five components with:

- **One `Registry`** (`internal/registry/informer/`) that manages lifecycle, refcounting,
  pool, and event broadcasting.
- **One `InformerManager`** (`internal/controller/informermanager/`) that exposes two
  `manager.Runnable` implementations sharing the same Registry.

### Rationale

- Eliminates ~600 lines of duplicated code.
- Single source of truth for "is this informer running?" and "who needs it?"
- The broadcast pattern (listeners) is more extensible than the old dispatcher routing.

---

## 2. Consumer naming pattern: `{prefix}:{policyKind}:{policyName}`

### Problem (the bug that motivated this section)

The first implementation of the unified registry used inconsistent consumer names:

- Sources: `"sources:/v1/configmaps"` — one consumer per GVR, regardless of how many
  policies reference it.
- Watched: `"watched:clustergenerationpolicies"` — one consumer per policy *type*,
  not per individual policy.

This meant:
1. **Sources refcount was inaccurate**: if 3 `ClusterGenerationPolicy` objects all referenced
   `/v1/configmaps` as a source, there was only 1 consumer. Deleting 2 of them wouldn't
   change the refcount. The informer would only die when the *last* policy of *any* type
   stopped referencing that GVR — but via an imprecise cleanup heuristic, not true refcounting.
2. **Watched consumers were not per-resource**: the consumer name didn't encode which
   GVRNN key it was about, so you couldn't tell *which policies* were keeping *which
   informers* alive.
3. **The two naming schemes were structurally different**, making the system harder to reason about.

### Decision

All consumer names follow a **uniform three-part pattern**:

```
{prefix}:{policyKind}:{policyName}
```

Examples:
```
sources:ClusterGenerationPolicy:gen-labels
sources:ClusterMutationPolicy:inject-sidecar
watched:ClusterGenerationPolicy:gen-labels
watched:ClusterCleanPolicy:clean-orphans
```

### Rationale

- **Exact refcounting**: when a `ClusterGenerationPolicy` named `gen-labels` is deleted,
  its reconciler removes it from the `PolicyStore`. On the next cleaner tick, the
  InformerManager sees that the consumer `sources:ClusterGenerationPolicy:gen-labels` is
  no longer in the desired set. It unregisters that consumer. If that was the last consumer
  for `/v1/configmaps`, the informer is disabled.
- **Orthogonal dimensions**: the informer key (`/v1/configmaps`) identifies *what* is
  watched. The consumer name identifies *who* needs it and *why* (sources vs watched).
  A single informer can serve both a sources consumer and a watched consumer if the
  key happens to overlap (future optimization path).
- **Uniform pattern**: both sources and watched use the same `{prefix}:{kind}:{name}`
  structure, making the code consistent and the refcount logic identical.

### How it works in practice

```
PolicyStore reconciler creates/updates policy "gen-labels"
  → stores it under collection key "apps/v1/deployments/default/nginx"
  → stores its source GVR "/v1/configmaps"

InformerManager.reconcileSources():
  → calls GetReferencedSourcesByPolicy() on all 4 policy stores
  → gets { "/v1/configmaps": ["gen-labels", "inject-sidecar"] }
  → registers consumers:
      "/v1/configmaps" ← "sources:ClusterGenerationPolicy:gen-labels"
      "/v1/configmaps" ← "sources:ClusterMutationPolicy:inject-sidecar"
  → launches informer if not started

InformerManager.reconcileWatched():
  → calls GetPolicyNamesByCollection() on generation + clean stores
  → gets { "apps/v1/deployments/default/nginx": ["gen-labels"] }
  → registers consumer:
      "apps/v1/deployments/default/nginx" ← "watched:ClusterGenerationPolicy:gen-labels"
  → launches informer if not started

User deletes policy "gen-labels":
  → reconciler removes it from PolicyStore
  → next cleaner tick: "sources:ClusterGenerationPolicy:gen-labels" is no longer desired
  → Unregister() called
  → if ConsumerCount == 0 → Disable() kills the informer
```

---

## 3. Why two Runnables instead of two controllers?

### Problem

The old `SourcesController` and `ObservedResourceController` were separate structs,
each implementing `manager.LeaderElectionRunnable`. They had different leader election
requirements:
- Sources: `NeedLeaderElection() = false` (all replicas need the pool for webhooks)
- Watched: `NeedLeaderElection() = true` (only leader should generate/clean)

### Decision

One `InformerManager` struct with two methods:
- `SourcesRunnable()` returns a `manager.Runnable` with `NeedLeaderElection() = false`
- `WatchedRunnable()` returns a `manager.Runnable` with `NeedLeaderElection() = true`

Both share the same `Registry` pointer and `Dependencies`.

### Rationale

- The reconcile/clean/launch logic is identical — only the data source (which registries
  to query) and leader election flag differ.
- Sharing the Registry means a sources informer and a watched informer for the same key
  reuse the same entry, avoiding redundant informers.
- The `InformerManager` is a single point of configuration (`Options`, `Dependencies`)
  instead of two separate structs with overlapping fields.

---

## 4. Broadcast + listeners instead of dispatcher + observer registry

### Problem

The old `EventDispatcher` maintained a `map[string]Processor` and looked up which processors
cared about a given resource via the `ResourceObserverRegistry`. This was a manual routing
table that had to be kept in sync with the informer registry.

### Decision

The `Registry` broadcasts all events to all registered `EventListener` implementations.
Each listener decides internally whether it cares:

- `PoolUpdater`: always cares (updates the sources pool for every event)
- `WatchedEventListener`: checks `HasAnyConsumerWithPrefix(key, "watched:{Kind}:")` to
  decide which processors to invoke.

### Rationale

- No separate routing table to maintain.
- Adding a new processor type = adding a new `WatchedProcessorEntry` in `main.go`.
  No registry changes needed.
- Fire-and-forget: each listener runs in its own goroutine, so a slow processor doesn't
  block the informer or other listeners.

---

## 5. SourcesPool interface

### Decision

Consumers of the sources cache (`FetchPolicySources`, `AdmissionServer`, processors)
depend on a minimal interface:

```go
type SourcesPool interface {
    GetPool(key ResourceKey) []*map[string]interface{}
}
```

`*Registry` satisfies this interface.

### Rationale

- Keeps the dependency narrow: consumers don't need to know about informer lifecycle,
  consumers map, or broadcasting.
- Makes testing easier: you can mock `SourcesPool` without creating a full Registry.
- `[]*map[string]interface{}` — pointers intentionally for zero-copy performance.
  The old code had the same design; we preserved it.

---

## 6. Buffered(1) StopSignal channel

### Problem

The old registries used `chan bool` for stop signals. The sender did a blocking send,
which could deadlock if the informer goroutine had already exited or hadn't started
listening yet.

### Decision

`StopSignal` is `chan struct{}` with buffer size 1. `Disable()` does a non-blocking send:

```go
select {
case entry.StopSignal <- struct{}{}:
default:
}
```

### Rationale

- If the informer is listening: it receives the signal and exits.
- If the informer has already exited: the send fills the buffer harmlessly.
- If the informer hasn't started listening yet: the signal is buffered, and the informer
  will receive it as soon as it starts the `<-entry.StopSignal` receive.
- No deadlock possible in any scenario.

---

## 7. GVKR in `internal/common/`

### Decision

The `GVKR` struct (GroupVersionKind + Resource + Subresource + Namespaced) was moved to
`internal/common/gvkr.go` as a shared type. Both `informermanager` and `observedresource`
packages use a type alias (`type GVKR = common.GVKR`).

### Rationale

- The struct was duplicated in `observedresource/controller_utils.go` and
  `informermanager/processors.go`. Since both packages need it and neither should
  depend on the other, a shared package is the right location.
- Using a type alias (`=`) instead of a type definition means the types are identical
  at the Go type system level — no conversion needed when passing between packages.

---

## 8. Complete event flow walkthrough

This section explains the full lifecycle: from a policy being created to events being
processed. Every list in a policy spec (watchedResources, interceptedResources, sources)
can have **multiple entries**, and the system handles each one independently.

### What a policy declares

A policy has up to three lists:

- **`watchedResources`** (generation/clean policies): Kubernetes resources to react to.
  Each entry is a GVRNN (group/version/resource/namespace/name). The policy is stored
  in the PolicyStore once per entry, under that GVRNN key.
- **`interceptedResources`** (validation/mutation policies): Kubernetes resources to
  intercept via admission webhooks. Each entry is a GVRO (group/version/resource/operation).
  The policy is stored once per unique GVRO key.
- **`sources`** (all policy types): auxiliary data the policy needs when it executes.
  Each entry is a GVR (group/version/resource). These are cached in memory by a dedicated
  informer so the template engine can read them without hitting the API server.

All three are lists. A single policy can watch 5 deployments, need 3 different source
types, and intercept 2 resource kinds — and the system tracks each combination independently.

### Step 1: Policy reconciliation (Kubebuilder controllers)

When a policy CR is created or updated, its Kubebuilder controller reconciles it:

```
ClusterGenerationPolicy "gen-labels" is created with:
  watchedResources:
    - apps/v1/deployments/default/nginx
    - /v1/pods/production/api-server
  sources:
    - /v1/configmaps
    - /v1/secrets

Controller iterates watchedResources and calls AddOrUpdateResource for each:
  PolicyStore["apps/v1/deployments/default/nginx"]    → [gen-labels]
  PolicyStore["/v1/pods/production/api-server"]        → [gen-labels]
```

Now the same policy object lives under two different collection keys in the PolicyStore.

### Step 2: InformerManager picks up the changes

Every 2 seconds, both runnables reconcile:

**SourcesRunnable** asks all 4 PolicyStores: "for each source GVR, which policies need it?"

```
GetReferencedSourcesByPolicy() iterates every policy's GetSources() list:
  "/v1/configmaps" → ["gen-labels"]     (gen-labels listed configmaps as a source)
  "/v1/secrets"    → ["gen-labels"]     (gen-labels listed secrets as a source)

reconcileSources() registers consumers:
  Registry.Register("/v1/configmaps", "sources:ClusterGenerationPolicy:gen-labels")
  Registry.Register("/v1/secrets",    "sources:ClusterGenerationPolicy:gen-labels")

Both informers are launched if not already running.
```

**WatchedRunnable** asks generation + clean PolicyStores: "for each collection key, which policies are in it?"

```
GetPolicyNamesByCollection() iterates the collections map:
  "apps/v1/deployments/default/nginx"  → ["gen-labels"]
  "/v1/pods/production/api-server"     → ["gen-labels"]

reconcileWatched() registers consumers:
  Registry.Register("apps/v1/deployments/default/nginx", "watched:ClusterGenerationPolicy:gen-labels")
  Registry.Register("/v1/pods/production/api-server",    "watched:ClusterGenerationPolicy:gen-labels")

Both informers are launched if not already running.
```

**Result: 4 informers running**, one per unique key. The same consumer name
(`...:gen-labels`) appears in multiple informer entries — that's correct, it means
this policy needs all of them.

### Step 3: An event arrives

Someone modifies the Deployment nginx. The watched informer detects the change:

```
kubeInformer fires UpdateFunc
  → handler extracts Unstructured objects (new + old)
  → Registry.Broadcast("apps/v1/deployments/default/nginx", Modified, newObj, oldObj)
```

Broadcast fires all listeners in parallel goroutines:

**PoolUpdater** receives the event:
```
PoolUpdater.OnEvent("apps/v1/deployments/default/nginx", Modified, newObj, oldObj)
  → RemoveFromPool (old object by namespace+name)
  → AddToPool (new object pointer)
```
This updates the in-memory cache. For watched resources the pool isn't typically
consumed, but it's maintained uniformly.

**WatchedEventListener** receives the event:
```
WatchedEventListener.OnEvent("apps/v1/deployments/default/nginx", Modified, newObj, oldObj)
  → For each processor entry:
    - GenerationProcessor (PolicyKind: "ClusterGenerationPolicy"):
      → registry.HasAnyConsumerWithPrefix(key, "watched:ClusterGenerationPolicy:")
      → YES — there's "watched:ClusterGenerationPolicy:gen-labels"
      → go GenerationProcessor.Process(key, Modified, newObj, oldObj)

    - CleanProcessor (PolicyKind: "ClusterCleanPolicy"):
      → registry.HasAnyConsumerWithPrefix(key, "watched:ClusterCleanPolicy:")
      → NO — no clean policy watches this key
      → skip
```

### Step 4: The processor executes

```
GenerationProcessor.Process("apps/v1/deployments/default/nginx", Modified, newObj, oldObj)

  1. Fetch policies: PolicyStore.GetResources("apps/v1/deployments/default/nginx")
     → returns [ClusterGenerationPolicy "gen-labels"]

  2. For each policy:
     a. Read policy.Spec.Sources → [/v1/configmaps, /v1/secrets]
     b. For EACH source GVR, call registry.GetPool(gvrKey):
        → GetPool("/v1/configmaps") → []*map[string]any (all cached configmaps)
        → GetPool("/v1/secrets")    → []*map[string]any (all cached secrets)
     c. Apply source filters (namespace, name, labels, regex) from the policy spec
     d. Inject into template context as .Sources

  3. Evaluate policy conditions against { .Object, .OldObject, .Sources, .Operation }
     → If conditions fail → skip this policy

  4. Render policy template (Go templates) → produces YAML

  5. Create or apply the rendered object in the cluster
```

### Step 5: Policy deletion and cleanup

When "gen-labels" is deleted:

```
Kubebuilder controller reconciles → removes policy from PolicyStore
  PolicyStore["apps/v1/deployments/default/nginx"]  → [] (empty)
  PolicyStore["/v1/pods/production/api-server"]      → [] (empty)
  (empty collections are auto-deleted)

Next cleaner tick (every 5s):
  cleanStaleConsumers("sources"):
    desired = getSourceConsumers() → {} (no policy references any source)
    For each registered key:
      "/v1/configmaps": UnregisterByPrefix("sources:") removes "sources:CGP:gen-labels"
        → ConsumerCount = 0 → Disable() → informer killed
      "/v1/secrets": same → informer killed

  cleanStaleConsumers("watched"):
    desired = getWatchedConsumers() → {} (no policy in any collection)
    For each registered key:
      "apps/v1/deployments/default/nginx": UnregisterByPrefix("watched:") → Disable()
      "/v1/pods/production/api-server": same → Disable()

All 4 informers are stopped and their entries removed from the Registry.
```

### Shared informers between policies

If two policies both need `/v1/configmaps` as a source:

```
Registry entry for "/v1/configmaps":
  consumers: {
    "sources:ClusterGenerationPolicy:gen-labels":      true,
    "sources:ClusterMutationPolicy:inject-sidecar":    true,
  }
```

Deleting `gen-labels` removes only its consumer. The informer stays alive because
`inject-sidecar` still needs it. Only when the last consumer is removed does the
informer shut down.

---

## 9. Future considerations

### Sources informer subsumption

A sources informer for `/v1/configmaps` watches *all* configmaps cluster-wide.
A watched informer for `/v1/configmaps/default/my-cm` only watches one specific configmap.
In theory, the sources informer could subsume the watched one — but implementing this
requires careful event routing (the watched consumer should only receive events for
objects matching its namespace/name filter). Deferred to a future iteration.

### Background mode for `interceptedResources` policies (TODO)

Currently, validation and mutation policies only act synchronously: they intercept
admission requests via webhooks and respond inline. The user wants to extend these
policies so they can also run in **background mode** (`background: true`).

In background mode, an interceptedResources policy would behave like a watched policy:
instead of (or in addition to) intercepting admission requests, it would watch the
matching resources via informers and evaluate/act asynchronously on every change.

**What needs to happen:**

1. Add a `background: bool` field to the validation/mutation policy specs.
2. When `background: true`, the Kubebuilder controller should also store the policy
   in a collection keyed by GVRNN (like generation/clean policies do today), not just GVRO.
3. The `WatchedRunnable` should query the validation/mutation PolicyStores in
   `getWatchedConsumers()` (currently it only queries generation + clean).
4. New `WatchedProcessorEntry` types would be needed for background validation and
   background mutation processing.
5. The consumer naming pattern already supports this: consumers would be named
   `"watched:ClusterValidationPolicy:my-policy"` and
   `"watched:ClusterMutationPolicy:my-policy"`, and the existing refcounting,
   prefix matching, and cleanup logic would work without changes.
6. The `SourcesRunnable` already queries all 4 PolicyStores, so sources informers
   for background policies are already handled.

**What does NOT need to change:**

- The Registry, consumer naming, broadcast, PoolUpdater, cleaner — all of these
  are already designed to be policy-kind-agnostic.
- The `HasAnyConsumerWithPrefix` routing in `WatchedEventListener` would pick up
  the new processor entries automatically.

### Sources informer subsumption

A sources informer for `/v1/configmaps` watches *all* configmaps cluster-wide.
A watched informer for `/v1/configmaps/default/my-cm` only watches one specific configmap.
In theory, the sources informer could subsume the watched one — but implementing this
requires careful event routing (the watched consumer should only receive events for
objects matching its namespace/name filter). Deferred to a future iteration.
