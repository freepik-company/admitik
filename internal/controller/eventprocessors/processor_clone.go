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

package eventprocessors

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/controller"
	"github.com/freepik-company/admitik/internal/globals"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/template"
)

// CloneProcessorDependencies holds the external dependencies required by CloneProcessor.
type CloneProcessorDependencies struct {
	ClusterClonePolicyRegistry  *policyStore.PolicyStore[*v1alpha1.ClusterClonePolicy]
	SourcesPool                 informerRegistry.SourcesPool
	KubeAvailableResourceListFn func() []GVKR
}

// CloneProcessor handles events for watched resources and clones objects into one or more
// target namespaces according to each matching ClusterClonePolicy.
type CloneProcessor struct {
	dependencies CloneProcessorDependencies
}

// NewCloneProcessor creates a CloneProcessor wired to the given dependencies.
func NewCloneProcessor(deps CloneProcessorDependencies) *CloneProcessor {
	return &CloneProcessor{dependencies: deps}
}

// Process is the entry point called by WatchedEventListener when a watched resource event arrives.
// For each matching ClusterClonePolicy it evaluates conditions, then either clones the rendered
// object into every target namespace or — when deleteOnConditionFalse is enabled — removes
// previously cloned objects.
func (p *CloneProcessor) Process(resourceType string, eventType watch.EventType, objects ...map[string]interface{}) {
	baseData := buildEventContext(eventType, objects)
	logger := newProcessorLogger(ObserverTypeClusterClonePolicies, objects[0], baseData.Operation)
	emitter := newEventEmitter(logger)

	deps := commonDeps{
		SourcesPool:             p.dependencies.SourcesPool,
		KubeAvailableResourceFn: p.dependencies.KubeAvailableResourceListFn,
	}

	for _, policy := range p.dependencies.ClusterClonePolicyRegistry.GetResources(resourceType) {
		policyLogger := logger.WithValues("ClusterClonePolicy", policy.Name)

		passed, evalData, err := evaluatePolicy(policy, &baseData, deps, policyLogger, emitter, objects[0])
		if err != nil {
			continue
		}

		if !passed {
			policyLogger.V(1).Info("conditions not met, skipping clone")
			if policy.Spec.DeleteOnConditionFalse {
				p.processCleanup(policy, evalData, policyLogger, emitter, objects[0])
			}
			continue
		}

		p.processClone(policy, evalData, policyLogger, emitter, objects[0])
	}
}

// processClone renders the object template, resolves its GVR, then creates or updates
// the cloned object in each target namespace.
func (p *CloneProcessor) processClone(
	policy *v1alpha1.ClusterClonePolicy,
	data *template.PolicyEvaluationDataT,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	obj, bd, gvr, errMsg := renderAndResolve(
		policy.Spec.Object.Engine,
		policy.Spec.Object.Template,
		data, p.dependencies.KubeAvailableResourceListFn(), logger,
	)
	if errMsg != "" {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:  "CloneAborted",
			Message: errMsg,
		})
		return
	}

	for _, targetNs := range policy.Spec.TargetNamespaces {
		p.cloneToNamespace(policy, obj, bd, gvr, targetNs.Namespace, logger, emitter, triggerObj)
	}
}

// cloneToNamespace creates or updates a single clone in the given namespace.
func (p *CloneProcessor) cloneToNamespace(
	policy *v1alpha1.ClusterClonePolicy,
	obj map[string]any,
	bd globals.ObjectBasicData,
	gvr schema.GroupVersionResource,
	targetNamespace string,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	nsLogger := logger.WithValues(
		"group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource,
		"name", bd.Name, "targetNamespace", targetNamespace,
	)

	cloneBd := bd
	cloneBd.Namespace = targetNamespace

	targetRef := common.TargetRefFromBasicData(cloneBd)

	clonedObj := deepCopyMap(obj)
	result := &unstructured.Unstructured{Object: clonedObj}
	result.SetNamespace(targetNamespace)
	stampCloneLabels(result, policy.Name)

	client := globals.Application.KubeRawClient.Resource(gvr).Namespace(targetNamespace)

	_, err := client.Create(globals.Application.Context, result, metav1.CreateOptions{})
	if err == nil {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CloneSucceeded",
			Message:   fmt.Sprintf("Object cloned to namespace %s", targetNamespace),
			TargetRef: targetRef,
		})
		return
	}

	if !errors.IsAlreadyExists(err) {
		nsLogger.Info("failed creating cloned object", "error", err.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CloneAborted",
			Message:   fmt.Sprintf("Object creation in namespace %s failed: %s", targetNamespace, err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	if !policy.Spec.OverwriteExisting {
		nsLogger.Info("object already exists and overwriteExisting is disabled")
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CloneAborted",
			Message:   fmt.Sprintf("Object already exists in namespace %s and overwriteExisting is disabled", targetNamespace),
			TargetRef: targetRef,
		})
		return
	}

	if _, err = client.Apply(globals.Application.Context, result.GetName(), result, metav1.ApplyOptions{
		FieldManager: controllerName,
		Force:        true,
	}); err != nil {
		nsLogger.Info("failed updating cloned object via server-side apply", "error", err.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CloneAborted",
			Message:   fmt.Sprintf("Object update in namespace %s via server-side apply failed: %s", targetNamespace, err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	emitter.Emit(triggerObj, policy, common.PolicyEvent{
		Action:    "CloneSucceeded",
		Message:   fmt.Sprintf("Object updated in namespace %s", targetNamespace),
		TargetRef: targetRef,
	})
}

// processCleanup renders the clone template to resolve the target object's identity,
// then removes cloned objects from each target namespace (only if they have ownership labels).
func (p *CloneProcessor) processCleanup(
	policy *v1alpha1.ClusterClonePolicy,
	data *template.PolicyEvaluationDataT,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	_, bd, gvr, errMsg := renderAndResolve(
		policy.Spec.Object.Engine,
		policy.Spec.Object.Template,
		data, p.dependencies.KubeAvailableResourceListFn(), logger,
	)
	if errMsg != "" {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:  "CloneCleanupAborted",
			Message: errMsg,
		})
		return
	}

	for _, targetNs := range policy.Spec.TargetNamespaces {
		p.cleanupFromNamespace(policy, bd, gvr, targetNs.Namespace, logger, emitter, triggerObj)
	}
}

// cleanupFromNamespace removes a single cloned object from the given namespace,
// verifying ownership labels before deletion.
func (p *CloneProcessor) cleanupFromNamespace(
	policy *v1alpha1.ClusterClonePolicy,
	bd globals.ObjectBasicData,
	gvr schema.GroupVersionResource,
	targetNamespace string,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	cloneBd := bd
	cloneBd.Namespace = targetNamespace
	targetRef := common.TargetRefFromBasicData(cloneBd)

	client := globals.Application.KubeRawClient.Resource(gvr).Namespace(targetNamespace)

	existing, err := client.Get(globals.Application.Context, bd.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Info("failed getting cloned object for cleanup", "error", err.Error(), "targetNamespace", targetNamespace)
			emitter.Emit(triggerObj, policy, common.PolicyEvent{
				Action:    "CloneCleanupAborted",
				Message:   fmt.Sprintf("Failed to get object in namespace %s for cleanup: %s", targetNamespace, err.Error()),
				TargetRef: targetRef,
			})
		}
		return
	}

	labels := existing.GetLabels()
	if labels[controller.ClonedByPolicyLabel] != policy.Name ||
		labels[controller.ClonedByPolicyKind] != controller.ClusterClonePolicyResourceType {
		return
	}

	if err = client.Delete(globals.Application.Context, bd.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		logger.Info("failed deleting cloned object during cleanup", "error", err.Error(), "targetNamespace", targetNamespace)
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CloneCleanupAborted",
			Message:   fmt.Sprintf("Object deletion in namespace %s during cleanup failed: %s", targetNamespace, err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	emitter.Emit(triggerObj, policy, common.PolicyEvent{
		Action:    "CloneCleanupSucceeded",
		Message:   fmt.Sprintf("Cloned object deleted from namespace %s because conditions are no longer met", targetNamespace),
		TargetRef: targetRef,
	})
}

// stampCloneLabels sets the admitik clone ownership labels on the given unstructured object.
func stampCloneLabels(obj *unstructured.Unstructured, policyName string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[controller.ClonedByPolicyLabel] = policyName
	labels[controller.ClonedByPolicyKind] = controller.ClusterClonePolicyResourceType
	obj.SetLabels(labels)
}

// deepCopyMap creates a deep copy of a map[string]any, recursively copying nested maps and slices.
func deepCopyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice creates a deep copy of a []any, recursively copying nested maps and slices.
func deepCopySlice(s []any) []any {
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}
