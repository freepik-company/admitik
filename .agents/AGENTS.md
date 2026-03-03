# AGENTS.md — Admitik

Admitik is a Kubernetes admission controller operator built with [Kubebuilder v4](https://book.kubebuilder.io/) and [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime). It provides policy-based validation, mutation, resource generation, and declarative cleanup through four cluster-scoped CRDs. Policies use a multi-engine templating system (CEL, Go templates, Starlark, plain text) for condition evaluation and patch/object generation.

**Module:** `github.com/freepik-company/admitik`
**API Group:** `admitik.dev`
**API Version:** `v1alpha1`
**Go Version:** 1.24

---

## Commands

### Build & Run

```bash
make build                # Build binary → bin/manager (runs manifests, generate, fmt, vet first)
make run                  # Run operator locally against a Kind cluster (requires Kind context + Caddy)
make docker-build         # Build container image (IMG=controller:latest by default)
make docker-push          # Push container image
make docker-buildx        # Multi-arch build+push (linux/arm64,amd64,s390x,ppc64le)
```

### Code Generation

```bash
make manifests            # Generate CRD YAML, RBAC, webhook configs via controller-gen
make generate             # Generate DeepCopy methods (zz_generated.deepcopy.go)
make fmt                  # go fmt ./...
make vet                  # go vet ./...
```

### Testing

```bash
make test                 # Unit/integration tests (uses envtest, excludes e2e, generates coverage)
make test-e2e             # E2E tests against a Kind cluster (Ginkgo v2, requires running cluster)
```

The unit test command expands to:
```bash
KUBEBUILDER_ASSETS="$(setup-envtest use 1.30.0 --bin-dir ./bin -p path)" \
  go test $(go list ./... | grep -v /e2e) -coverprofile cover.out
```

### Linting

```bash
make lint                 # golangci-lint run
make lint-fix             # golangci-lint run --fix
```

Linter config: `.golangci.yml` — disables all linters then explicitly enables ~20 (including `ginkgolinter`, `goimports`, `govet`, `staticcheck`, `unused`). Excludes `lll` for `api/*` and `dupl`+`lll` for `internal/*`.

### Deployment (Kubernetes)

```bash
make install              # Install CRDs into cluster (kustomize build config/crd | kubectl apply)
make uninstall            # Remove CRDs
make deploy               # Deploy controller to cluster (kustomize build config/default | kubectl apply)
make undeploy             # Remove controller deployment
make build-installer      # Generate consolidated install YAML → dist/install.yaml
```

### Tool Dependencies

All tools auto-download to `./bin/` on first use:
- `controller-gen` v0.15.0
- `kustomize` v5.4.1
- `setup-envtest` release-0.18
- `golangci-lint` v1.57.2
- `caddy` v2.10.0 (for local dev reverse proxy)

---

## Project Structure

```
cmd/main.go                          # Entrypoint — wires all controllers, registries, admission server
api/v1alpha1/                        # CRD type definitions (Kubebuilder markers)
  clustervalidationpolicy_types.go   # ClusterValidationPolicy spec/status
  clustermutationpolicy_types.go     # ClusterMutationPolicy spec/status (with Priority, Patch)
  clustergenerationpolicy_types.go   # ClusterGenerationPolicy spec/status (with WatchedResources, Object)
  clustercleanpolicy_types.go        # ClusterCleanPolicy spec/status (with WatchedResources, Target)
  common_types.go                    # Shared types: SourceGroupT, ResourceGroupT, ConditionT, etc.
  groupversion_info.go               # GroupVersion registration (admitik.dev/v1alpha1)
  zz_generated.deepcopy.go           # Auto-generated — DO NOT EDIT

internal/
  controller/
    commons.go                       # Shared constants, GetWebhookClientConfig, UpdateWithRetry
    conditions.go                    # Status condition helpers
    clustervalidationpolicy/         # Reconciler: syncs ValidatingWebhookConfiguration
    clustermutationpolicy/           # Reconciler: syncs MutatingWebhookConfiguration (priority-sorted)
    clustergenerationpolicy/         # Reconciler: registry-only (no webhook config), auto-cleanup on delete
    clustercleanpolicy/              # Reconciler: registry-only, declarative cleanup of target resources
    informermanager/                 # Unified InformerManager: sources + watched runnable (replaces old controllers)
    eventprocessors/                 # Event processors (pool updater, generation, clean) and GVKR utilities

  server/admission/                  # HTTP admission server (validation + mutation handlers)
    server.go                        # HttpServer, route setup, TLS
    server_controller.go             # AdmissionServer (manager.Runnable, no leader election)
    handler_common.go                # Shared admission request parsing
    handler_validation.go            # Validation logic (enforce/permissive)
    handler_mutation.go              # Mutation logic (jsonpatch/jsonmerge/strategicmerge)

  registry/
    policystore/                     # Generic PolicyStore[T] — in-memory policy index by collection key
    informer/                        # Unified informer Registry — lifecycle, refcounting, pool, event broadcast

  template/                          # Multi-engine template evaluation
    template.go                      # EvaluateTemplate dispatcher
    cel.go                           # CEL engine (google/cel-go)
    gotmpl.go                        # Go templates with Sprig + Helm funcs
    starlark.go                      # Starlark interpreter with module library
    plain.go                         # Plain text + optional {{cel: expr}} interpolation
    types.go                         # InjectedDataI, PolicyEvaluationDataT, TriggerInjectedDataT
    starlarkmods/yaml/               # Custom Starlark YAML module (encode/decode)

  strategicmerge/                    # Kubernetes strategic merge patch implementation
  certificates/                      # Self-signed TLS certificate generation
  globals/                           # Application-wide state (Kubernetes clients, context)
  common/                            # Shared utilities (conditions, events, normalization, source fetching, cleanup)

config/                              # Kustomize overlays
  default/                           # Root overlay (composes crd + rbac + manager)
  crd/                               # CRD base manifests
  rbac/                              # RBAC (ClusterRole, ServiceAccount, leader election)
  manager/                           # Controller-manager Deployment
  prometheus/                        # ServiceMonitor for metrics
  caddy/                             # Caddyfile for local dev TLS proxy

charts/admitik/                      # Helm chart (v1.9.1)
test/e2e/                            # Ginkgo e2e tests (require Kind cluster)
test/utils/                          # Test helper functions
docs/                                # Documentation, samples, proposals
```

---

## Architecture

### CRDs (all cluster-scoped)

| CRD | Purpose | Key Fields |
|-----|---------|------------|
| `ClusterValidationPolicy` | Validate admission requests | `interceptedResources`, `conditions`, `message`, `failureAction` (enforce/permissive) |
| `ClusterMutationPolicy` | Mutate admission requests | `interceptedResources`, `conditions`, `patch` (type + template), `priority` |
| `ClusterGenerationPolicy` | Generate resources on watched changes | `watchedResources`, `conditions`, `object.definition`, `overwriteExisting`, `deleteOnConditionFalse`, `conditionRecheckInterval` |
| `ClusterCleanPolicy` | Delete resources when conditions are met on watched changes | `watchedResources`, `conditions`, `target` (engine + template), `conditionRecheckInterval` |

### Data Flow

```
Policy CRDs ──reconcile──► PolicyStore (4 generic instances)
                                │
                ┌───────────────┼───────────────────┐
                ▼               ▼                   ▼
        InformerManager     AdmissionServer    InformerManager
        SourcesRunnable     (all replicas)     WatchedRunnable
        (all replicas)          │              (leader-elected)
                │               │                   │
                ▼               │                   ▼
        Unified Registry ◄─────┘         Broadcast → Listeners
        PoolUpdater (pool)                      │
                                    ┌───────────┴──────────┐
                                    ▼                      ▼
                            GenerationProcessor    CleanProcessor
                                    │                      │
                                    ▼                      ▼
                             Create/Update           Delete target
                              resources               resource
                                    └──────────┬──────────┘
                                               ▼
                                        Kubernetes API

ConditionRecheckRunnable (leader-elected)
  - Per-policy ticker goroutine (interval from conditionRecheckInterval)
  - On tick: reads pool objects, fires synthetic Modified events to processors
  - Reconciles goroutines every 2s (detect add/remove/interval change)
```

### Registry Key Patterns

- **Admission policies:** `{group}/{version}/{resource}/{operation}` (e.g., `apps/v1/deployments/CREATE`)
- **Generation policies:** `{group}/{version}/{resource}/{namespace}/{name}` (e.g., `/v1/configmaps/default/my-cm`)
- **Clean policies:** `{group}/{version}/{resource}/{namespace}/{name}` (same GVRNN pattern as generation)
- **Source informers:** `{group}/{version}/{resource}` (e.g., `/v1/configmaps`)

### Controller Patterns

Every reconciler follows a 7-step pattern:
1. Get resource from cluster
2. Check if found (ignore not-found for deletions)
3. Handle deletion: clean up registry + finalizer, return
4. Add finalizer if missing
5. Defer status condition update
6. Reconcile (sync to registry, optionally create webhook config)
7. Set success condition

All controllers use:
- `predicate.GenerationChangedPredicate{}` — only reconcile on spec changes
- `NeedLeaderElection: false` — run on all replicas (except ObservedResourceController which requires leader)
- Options/Dependencies struct separation for configuration vs shared state injection

### Template Engines

| Engine | Value | Use Case |
|--------|-------|----------|
| `cel` | `"cel"` | CEL expressions (default for conditions) |
| `gotmpl` | `"gotmpl"` | Go templates with Sprig + Helm-like funcs (`toYaml`, `fromYaml`, `toJson`, `fromJson`, `toToml`, `logPrintf`) |
| `starlark` | `"starlark"` | Starlark scripts (with math, json, time, base64, csv, hashlib, http, log, net, random, re, string, yaml modules) |
| `plain` | `"plain"` | Passthrough (no processing) |
| `plain+cel` | `"plain+cel"` | Plain text with `{{cel: expr}}` inline interpolation |

Template data injected as `PolicyEvaluationDataT`: `Operation`, `Object`, `OldObject`, `Sources`, `Vars`.

### Mutation Patch Types

| Type | Format | Description |
|------|--------|-------------|
| `jsonpatch` | JSON | RFC 6902 JSON Patch (default) |
| `jsonmerge` | YAML input → JSON Merge Patch | RFC 7386 |
| `strategicmerge` | YAML input → Strategic Merge | Kubernetes-native merge using OpenAPI schemas |

Mutation policies are sorted by `Spec.Priority` (ascending) and patches are applied incrementally.

---

## Code Conventions

### Go Style
- Standard `gofmt`/`goimports` formatting (enforced by lint)
- Apache 2.0 license header on all source files (see `hack/boilerplate.go.txt`)
- Kubebuilder markers for CRD generation (`+kubebuilder:object:root=true`, `+kubebuilder:resource:`, `+listType=map`, etc.)
- Types suffixed with `T` for non-Kubernetes types: `ConditionT`, `SourceGroupT`, `PatchT`, `MessageT`, etc.
- Unexported application singleton: `globals.Application` (holds context + Kubernetes clients)

### Controller Structure
Each controller lives in its own subpackage under `internal/controller/` with 3 files:
- `controller.go` — Reconciler struct, `Reconcile()`, `SetupWithManager()`
- `sync.go` — Core reconciliation logic (registry sync, webhook config)
- `status.go` — Status condition update helpers

### Registry Pattern
- Unified `informer.Registry` with `sync.RWMutex` at registry level + per-entry `sync.Mutex` for pool access
- Refcount-based informer lifecycle: consumers follow the pattern `{prefix}:{policyKind}:{policyName}` (e.g. `sources:ClusterGenerationPolicy:gen-labels`); informer dies when refcount = 0
- `PoolUpdater` (in `eventprocessors/`) listens to broadcast events and maintains sources cache (`[]*map[string]any` for zero-copy performance)
- `WatchedEventListener` routes events to generation/clean processors using prefix-based consumer matching
- Generic `PolicyStore[T PolicyResourceI]` parameterized by CRD type
- See `.agents/DESIGN_DECISIONS.md` for detailed rationale on consumer naming, broadcast pattern, etc.

### Error Handling
- Controllers use format-string constants for error messages (defined in `internal/controller/commons.go`)
- `UpdateWithRetry()` uses exponential backoff (5 steps, 200ms initial, 2x factor) for conflict resolution
- Admission server defaults: validation → `Allowed: false` (fail-closed), mutation → `Allowed: true` (fail-open)

### Naming
- CRD kinds: `ClusterValidationPolicy`, `ClusterMutationPolicy`, `ClusterGenerationPolicy`, `ClusterCleanPolicy`
- Webhook configs: `admitik-cluster-validation-policy`, `admitik-cluster-mutation-policy`
- Finalizer: `admitik.dev/finalizer`
- Ignore label: `admitik.dev/ignore-admission`
- Ownership labels: `admitik.dev/generated-by` (policy name), `admitik.dev/generated-by-kind` (policy kind)
- Admission paths: `/admission/validate`, `/admission/mutate`
- Namespace: `admitik-system` (kustomize default)

---

## Testing

### Unit/Integration Tests
- Framework: envtest (Kubebuilder) + Ginkgo v2 + Gomega
- Run: `make test` (auto-downloads envtest binaries for K8s 1.30.0)
- Coverage output: `cover.out`
- Excludes `test/e2e/` packages

### E2E Tests
- Framework: Ginkgo v2 + Gomega, `Ordered` suite
- Requires: Kind cluster, Docker
- Run: `make test-e2e` or `go test ./test/e2e/ -v -ginkgo.v`
- Setup: installs Prometheus Operator + cert-manager, builds and loads Docker image to Kind, deploys via `make install` + `make deploy`
- Test helpers in `test/utils/utils.go`: `Run()`, `LoadImageToKindClusterWithName()`, `GetProjectDir()`
- `KIND_CLUSTER` env var controls cluster name (default: `"kind"`)

---

## Local Development

### Prerequisites
- Go 1.24+
- Docker
- kubectl
- Kind (`kind create cluster`)

### Workflow
1. `kind create cluster`
2. `make install run` — installs CRDs and runs the operator locally
   - The `make run` target validates you're on a Kind context, starts a Caddy TLS reverse proxy (`:8443` → `:10250`), and runs the controller with debug logging
   - Caddy provides the TLS termination needed for Kubernetes to reach the local webhook server
3. `kubectl apply -k ./docs/samples/` — apply sample policies for testing

### Key Flags (for `make run`)
- `--webhook-client-hostname` — hostname Kubernetes uses to reach webhooks (auto-detected from Docker bridge)
- `--webhook-client-port` — port Kubernetes uses (default: 10250, overridden to 8443 for local dev)
- `--webhook-server-ca` — CA bundle for webhook TLS (uses Caddy's local CA in dev)
- `--zap-log-level=debug` — verbose logging

---

## CI/CD

All workflows trigger on GitHub `release` events + `workflow_dispatch`:

| Workflow | Action |
|----------|--------|
| `release-binaries.yaml` | Cross-compile Go binaries, attach to release |
| `release-bundle.yaml` | Generate `dist/install.yaml` manifest bundle |
| `release-charts.yaml` | Publish Helm chart to GitHub Pages |
| `release-docker-images.yaml` | Multi-arch Docker build, push to `ghcr.io/freepik-company/admitik` |

---

## Gotchas

- **`zz_generated.deepcopy.go` is auto-generated.** Never edit it. Run `make generate` after changing API types.
- **CRD manifests are generated.** Run `make manifests` after changing Kubebuilder markers in `api/v1alpha1/`.
- **`make build` runs `manifests`, `generate`, `fmt`, `vet` as prerequisites.** If you only want to compile, use `go build -o bin/manager cmd/main.go` directly.
- **All admission controllers run without leader election.** Every replica processes all policies independently. The `InformerManager.WatchedRunnable()` (generation/clean) is leader-elected; `InformerManager.SourcesRunnable()` (pool caching) runs on all replicas.
- **Starlark engine triggers GC after every evaluation** (`runtime.GC()` + `debug.FreeOSMemory()`). Be aware of performance implications in high-throughput mutation/validation paths.
- **Strategic merge patches fetch OpenAPI schemas** from the cluster's discovery client, cached with 5-second refresh. First request after startup may be slower.
- **The `env` and `expandenv` Sprig functions are explicitly removed** from Go template evaluation for security.
- **Mutation policies are order-sensitive.** They sort by `Spec.Priority` ascending — lower numbers execute first. Patches accumulate incrementally.
- **Validation defaults to fail-closed** (`Allowed: false`), mutation defaults to fail-open (`Allowed: true`).
- **Source filters support inline CEL expressions.** Filter fields (namespace, name, labels) can contain `{{cel: expr}}` that get resolved at evaluation time via recursive reflection.
- **The `globals.Application` singleton** holds shared Kubernetes clients and context. It's initialized in `cmd/main.go` and accessed across packages.
- **Local dev requires Caddy** for TLS termination. The `make run` target auto-installs it to `./bin/` and starts it as a reverse proxy.
- **Generated resources are labeled for tracking.** `admitik.dev/generated-by=<policyName>` and `admitik.dev/generated-by-kind=ClusterGenerationPolicy` are stamped on all resources created by generation policies.
- **Auto-cleanup on generation policy deletion** is controlled by `--cleanup-on-generation-policy-delete` flag (default: `true`). When enabled, deleting a `ClusterGenerationPolicy` scans all API resources for matching ownership labels and deletes them.
- **`ClusterCleanPolicy` cleanup scans API resources.** The `CleanupGeneratedResources` helper discovers all API resource types, which can be slow on clusters with many CRDs. Future optimization may cache or narrow the scope.
- **`ClusterCleanPolicy` follows the same architecture** as `ClusterGenerationPolicy` — it registers watched resources, triggers on events, evaluates conditions, and uses template-based target resolution for deletion.
- **`deleteOnConditionFalse` (ClusterGenerationPolicy only):** When `true`, if conditions evaluate to `false` the processor renders the generation template to resolve the target object's identity and deletes it — but only if the object carries the policy ownership labels (`admitik.dev/generated-by` + `admitik.dev/generated-by-kind`). Safe: never deletes objects not owned by the policy.
- **`conditionRecheckInterval` (ClusterGenerationPolicy + ClusterCleanPolicy):** Sets a `time.Duration` for periodic condition re-evaluation even without a watched-resource event. Implemented by `ConditionRecheckRunnable` (leader-elected, in `internal/controller/informermanager/processors.go`). Each policy with a non-zero interval gets its own ticker goroutine; on tick it reads pool objects and fires synthetic `Modified` events to the processor. Goroutines are reconciled every 2s to handle interval changes or policy additions/deletions. Adding a new policy type to the recheck just requires a new `RecheckEntry` in `cmd/main.go`.
- **"Conditions not met" is silent.** No log is emitted when conditions evaluate to `false` in any processor or handler — this would produce too much noise during periodic recheck ticks. Condition evaluation *errors* (broken template) are logged at `V(1)` in background processors and at `Info` in synchronous admission handlers.

---

## Samples

Sample policies are in `docs/samples/` organized by CRD type. Apply all with:
```bash
kubectl apply -k ./docs/samples/
```

---

## TODO / Future Plans

- **Unify `interceptedResources` and `watchedResources`**: In the future, `interceptedResources` will be renamed to `watchedResources` (or similar) with a `background: true/false` field to indicate whether the resource goes through an admission webhook or is handled by background informers. This allows a single consistent API surface for all policy types. Keep this in mind when refactoring registry/controller code — design for this convergence.
