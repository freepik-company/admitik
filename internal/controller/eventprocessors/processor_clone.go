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
	"slices"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/controller"
	"github.com/freepik-company/admitik/internal/globals"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
)

// CloneProcessorDependencies holds the external dependencies required by CloneProcessor.
type CloneProcessorDependencies struct {
	ClusterClonePolicyRegistry  *policyStore.PolicyStore[*v1alpha1.ClusterClonePolicy]
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
// For each matching ClusterClonePolicy it evaluates conditions and then:
//   - On DELETE: unconditionally removes cloned objects from every target namespace.
//   - On CREATE/UPDATE with passing conditions: clones the watched object into every target namespace.
//   - When conditions are not met and deleteOnConditionFalse is enabled: removes cloned objects.
func (p *CloneProcessor) Process(resourceType string, eventType watch.EventType, objects ...map[string]interface{}) {
	baseData := buildEventContext(eventType, objects)
	logger := newProcessorLogger(ObserverTypeClusterClonePolicies, objects[0], baseData.Operation)
	emitter := newEventEmitter(logger)

	gvr := gvrFromResourceType(resourceType)

	for _, policy := range p.dependencies.ClusterClonePolicyRegistry.GetResources(resourceType) {
		policyLogger := logger.WithValues("ClusterClonePolicy", policy.Name)

		bd, err := globals.GetObjectBasicData(&objects[0])
		if err != nil {
			policyLogger.Info("failed extracting metadata from watched object", "error", err.Error())
			emitter.Emit(objects[0], policy, common.PolicyEvent{
				Action:  "CloneAborted",
				Message: fmt.Sprintf("Failed to extract object metadata: %s", err.Error()),
			})
			continue
		}

		targetNamespaces, err := resolveTargetNamespaces(policy.Spec.Target, bd.Namespace, policyLogger)
		if err != nil {
			policyLogger.Info("failed resolving target namespaces", "error", err.Error())
			emitter.Emit(objects[0], policy, common.PolicyEvent{
				Action:  "CloneAborted",
				Message: fmt.Sprintf("Failed to resolve target namespaces: %s", err.Error()),
			})
			continue
		}

		if len(targetNamespaces) == 0 {
			policyLogger.V(1).Info("no target namespaces matched")
			continue
		}

		if baseData.Operation == common.NormalizedOperationDelete {
			p.processDeleteAll(policy, objects[0], bd, gvr, targetNamespaces, policyLogger, emitter)
			continue
		}

		passed, condErr := common.IsPassingConditions(policy.GetConditions(), &baseData)
		if condErr != nil {
			policyLogger.V(1).Info("failed evaluating conditions", "error", condErr.Error())
			emitter.Emit(objects[0], policy, common.PolicyEvent{
				Action:  "ConditionEvaluationFailed",
				Message: condErr.Error(),
			})
			continue
		}

		if !passed {
			policyLogger.V(1).Info("conditions not met, skipping clone")
			if policy.Spec.DeleteOnConditionFalse {
				p.processDeleteAll(policy, objects[0], bd, gvr, targetNamespaces, policyLogger, emitter)
			}
			continue
		}

		p.processClone(policy, objects[0], bd, gvr, targetNamespaces, policyLogger, emitter)
	}
}

// resolveTargetNamespaces resolves the target namespace selectors into a deduplicated list
// of namespace names. Multiple target entries are ORed. Within a single entry, selectors
// are ANDed. The source namespace is automatically excluded.
func resolveTargetNamespaces(targets []v1alpha1.CloneTargetT, sourceNamespace string, logger logr.Logger) ([]string, error) {
	allNamespaces, err := globals.Application.KubeRawCoreClient.CoreV1().Namespaces().List(
		globals.Application.Context, metav1.ListOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed listing namespaces: %w", err)
	}

	result := map[string]bool{}

	for _, target := range targets {
		matched := matchNamespaces(allNamespaces.Items, target.Namespace, logger)
		for _, ns := range matched {
			if ns != sourceNamespace {
				result[ns] = true
			}
		}
	}

	namespaces := make([]string, 0, len(result))
	for ns := range result {
		namespaces = append(namespaces, ns)
	}
	slices.Sort(namespaces)
	return namespaces, nil
}

// matchNamespaces filters cluster namespaces against a single CloneTargetNamespaceSelectorT.
// All specified selectors within the entry are ANDed.
func matchNamespaces(allNamespaces []corev1.Namespace, selector v1alpha1.CloneTargetNamespaceSelectorT, logger logr.Logger) []string {
	var matched []string

	for _, ns := range allNamespaces {
		if matchesSelector(ns, selector, logger) {
			matched = append(matched, ns.Name)
		}
	}

	return matched
}

// matchesSelector returns true if a namespace satisfies all criteria in the selector (AND).
func matchesSelector(ns corev1.Namespace, selector v1alpha1.CloneTargetNamespaceSelectorT, logger logr.Logger) bool {
	if len(selector.Names) > 0 {
		if !slices.Contains(selector.Names, ns.Name) {
			return false
		}
	}

	if selector.LabelSelector != nil {
		sel, err := metav1.LabelSelectorAsSelector(selector.LabelSelector)
		if err != nil {
			logger.V(1).Info("invalid labelSelector, skipping", "error", err.Error())
			return false
		}
		if !sel.Matches(labels.Set(ns.Labels)) {
			return false
		}
	}

	if len(selector.AnnotationSelector) > 0 {
		for k, v := range selector.AnnotationSelector {
			if ns.Annotations[k] != v {
				return false
			}
		}
	}

	return true
}

// processClone takes the watched object, strips runtime metadata, and clones it
// into each target namespace.
func (p *CloneProcessor) processClone(
	policy *v1alpha1.ClusterClonePolicy,
	triggerObj map[string]interface{},
	bd globals.ObjectBasicData,
	gvr schema.GroupVersionResource,
	targetNamespaces []string,
	logger logr.Logger,
	emitter *common.EventEmitter,
) {
	for _, targetNs := range targetNamespaces {
		p.cloneToNamespace(policy, triggerObj, bd, gvr, targetNs, logger, emitter)
	}
}

// cloneToNamespace creates or updates a single clone in the given namespace.
func (p *CloneProcessor) cloneToNamespace(
	policy *v1alpha1.ClusterClonePolicy,
	triggerObj map[string]interface{},
	bd globals.ObjectBasicData,
	gvr schema.GroupVersionResource,
	targetNamespace string,
	logger logr.Logger,
	emitter *common.EventEmitter,
) {
	nsLogger := logger.WithValues(
		"group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource,
		"name", bd.Name, "targetNamespace", targetNamespace,
	)

	cloneBd := bd
	cloneBd.Namespace = targetNamespace
	targetRef := common.TargetRefFromBasicData(cloneBd)

	clonedObj := deepCopyMap(triggerObj)
	result := &unstructured.Unstructured{Object: clonedObj}
	stripRuntimeMetadata(result)
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

// processDeleteAll removes cloned objects from all target namespaces when the source
// object is deleted or conditions are no longer met.
func (p *CloneProcessor) processDeleteAll(
	policy *v1alpha1.ClusterClonePolicy,
	triggerObj map[string]interface{},
	bd globals.ObjectBasicData,
	gvr schema.GroupVersionResource,
	targetNamespaces []string,
	logger logr.Logger,
	emitter *common.EventEmitter,
) {
	for _, targetNs := range targetNamespaces {
		p.cleanupFromNamespace(policy, bd, gvr, targetNs, logger, emitter, triggerObj)
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

	objLabels := existing.GetLabels()
	if objLabels[controller.ClonedByPolicyLabel] != policy.Name ||
		objLabels[controller.ClonedByPolicyKind] != controller.ClusterClonePolicyResourceType {
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
		Message:   fmt.Sprintf("Cloned object deleted from namespace %s because source was deleted or conditions are no longer met", targetNamespace),
		TargetRef: targetRef,
	})
}

// gvrFromResourceType extracts the GroupVersionResource from a GVRNN resource type key.
// The key format is "{group}/{version}/{resource}/{namespace}/{name}".
func gvrFromResourceType(resourceType string) schema.GroupVersionResource {
	parts := strings.Split(resourceType, "/")
	if len(parts) >= 3 {
		return schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: parts[2],
		}
	}
	return schema.GroupVersionResource{}
}

// stripRuntimeMetadata removes Kubernetes runtime-managed fields from an object
// so it can be cleanly created in a different namespace.
func stripRuntimeMetadata(obj *unstructured.Unstructured) {
	obj.SetResourceVersion("")
	obj.SetUID("")
	obj.SetCreationTimestamp(metav1.Time{})
	obj.SetDeletionTimestamp(nil)
	obj.SetDeletionGracePeriodSeconds(nil)
	obj.SetGenerateName("")
	obj.SetSelfLink("")
	obj.SetManagedFields(nil)
	obj.SetOwnerReferences(nil)
	obj.SetFinalizers(nil)

	annotations := obj.GetAnnotations()
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
	if len(annotations) == 0 {
		annotations = nil
	}
	obj.SetAnnotations(annotations)

	unstructured.RemoveNestedField(obj.Object, "status")
}

// stampCloneLabels sets the admitik clone ownership labels on the given unstructured object.
func stampCloneLabels(obj *unstructured.Unstructured, policyName string) {
	l := obj.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	l[controller.ClonedByPolicyLabel] = policyName
	l[controller.ClonedByPolicyKind] = controller.ClusterClonePolicyResourceType
	obj.SetLabels(l)
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
