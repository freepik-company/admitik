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
	"k8s.io/apimachinery/pkg/watch"

	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/controller"
	"github.com/freepik-company/admitik/internal/globals"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/template"
)

// GenerationProcessorDependencies holds the external dependencies required by GenerationProcessor.
type GenerationProcessorDependencies struct {
	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	SourcesPool                     informerRegistry.SourcesPool
	KubeAvailableResourceListFn     func() []GVKR
}

// GenerationProcessor handles events for watched resources and creates, updates, or deletes
// generated objects according to each matching ClusterGenerationPolicy.
type GenerationProcessor struct {
	dependencies GenerationProcessorDependencies
}

// NewGenerationProcessor creates a GenerationProcessor wired to the given dependencies.
func NewGenerationProcessor(deps GenerationProcessorDependencies) *GenerationProcessor {
	return &GenerationProcessor{dependencies: deps}
}

// Process is the entry point called by WatchedEventListener when a watched resource event arrives.
// For each matching ClusterGenerationPolicy it evaluates conditions, then either generates/updates
// the target object or — when deleteOnConditionFalse is enabled — cleans it up.
func (p *GenerationProcessor) Process(resourceType string, eventType watch.EventType, objects ...map[string]interface{}) {
	baseData := buildEventContext(eventType, objects)
	logger := newProcessorLogger(ObserverTypeClusterGenerationPolicies, objects[0], baseData.Operation)
	emitter := newEventEmitter(logger)

	deps := commonDeps{
		SourcesPool:             p.dependencies.SourcesPool,
		KubeAvailableResourceFn: p.dependencies.KubeAvailableResourceListFn,
	}

	for _, policy := range p.dependencies.ClusterGenerationPolicyRegistry.GetResources(resourceType) {
		policyLogger := logger.WithValues("ClusterGenerationPolicy", policy.Name)

		passed, evalData, err := evaluatePolicy(policy, &baseData, deps, policyLogger, emitter, objects[0])
		if err != nil {
			continue
		}

		if !passed {
			policyLogger.V(1).Info("conditions not met, skipping generation")
			if policy.Spec.DeleteOnConditionFalse {
				p.processCleanup(policy, evalData, policyLogger, emitter, objects[0])
			}
			continue
		}

		p.processGeneration(policy, evalData, policyLogger, emitter, objects[0])
	}
}

// processGeneration renders the object template, stamps ownership labels, and creates or
// updates (server-side apply) the resulting Kubernetes resource.
func (p *GenerationProcessor) processGeneration(
	policy *v1alpha1.ClusterGenerationPolicy,
	data *template.PolicyEvaluationDataT,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	obj, bd, gvr, errMsg := renderAndResolve(
		policy.Spec.Object.Definition.Engine,
		policy.Spec.Object.Definition.Template,
		data, p.dependencies.KubeAvailableResourceListFn(), logger,
	)
	if errMsg != "" {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:  "GenerationAborted",
			Message: errMsg,
		})
		return
	}

	targetRef := common.TargetRefFromBasicData(bd)

	logger = logger.WithValues(
		"group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource,
		"name", bd.Name, "namespace", bd.Namespace,
	)

	result := &unstructured.Unstructured{Object: obj}
	stampOwnershipLabels(result, policy.Name)

	client := globals.Application.KubeRawClient.Resource(gvr).Namespace(bd.Namespace)

	_, err := client.Create(globals.Application.Context, result, metav1.CreateOptions{})
	if err == nil {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "GenerationSucceeded",
			Message:   "Object created successfully",
			TargetRef: targetRef,
		})
		return
	}

	if !errors.IsAlreadyExists(err) {
		logger.Info("failed creating generated object", "error", err.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "GenerationAborted",
			Message:   fmt.Sprintf("Object creation failed: %s", err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	if !policy.Spec.OverwriteExisting {
		logger.Info("object already exists and overwriteExisting is disabled")
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "GenerationAborted",
			Message:   "Object already exists and overwriteExisting is disabled",
			TargetRef: targetRef,
		})
		return
	}

	if _, err = client.Apply(globals.Application.Context, result.GetName(), result, metav1.ApplyOptions{
		FieldManager: controllerName,
		Force:        true,
	}); err != nil {
		logger.Info("failed updating generated object via server-side apply", "error", err.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "GenerationAborted",
			Message:   fmt.Sprintf("Object update via server-side apply failed: %s", err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	emitter.Emit(triggerObj, policy, common.PolicyEvent{
		Action:    "GenerationSucceeded",
		Message:   "Object updated successfully",
		TargetRef: targetRef,
	})
}

// processCleanup renders the generation template to resolve the target object's identity,
// verifies ownership labels, and deletes it. Safe: never touches objects not owned by this policy.
func (p *GenerationProcessor) processCleanup(
	policy *v1alpha1.ClusterGenerationPolicy,
	data *template.PolicyEvaluationDataT,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	_, bd, gvr, errMsg := renderAndResolve(
		policy.Spec.Object.Definition.Engine,
		policy.Spec.Object.Definition.Template,
		data, p.dependencies.KubeAvailableResourceListFn(), logger,
	)
	if errMsg != "" {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:  "CleanupAborted",
			Message: errMsg,
		})
		return
	}

	targetRef := common.TargetRefFromBasicData(bd)
	client := globals.Application.KubeRawClient.Resource(gvr).Namespace(bd.Namespace)

	existing, err := client.Get(globals.Application.Context, bd.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Info("failed getting object for cleanup", "error", err.Error())
			emitter.Emit(triggerObj, policy, common.PolicyEvent{
				Action:    "CleanupAborted",
				Message:   fmt.Sprintf("Failed to get object for cleanup: %s", err.Error()),
				TargetRef: targetRef,
			})
		}
		return
	}

	labels := existing.GetLabels()
	if labels[controller.GeneratedByPolicyLabel] != policy.Name ||
		labels[controller.GeneratedByPolicyKind] != controller.ClusterGenerationPolicyResourceType {
		return
	}

	if err = client.Delete(globals.Application.Context, bd.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		logger.Info("failed deleting generated object during cleanup", "error", err.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CleanupAborted",
			Message:   fmt.Sprintf("Object deletion during cleanup failed: %s", err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	emitter.Emit(triggerObj, policy, common.PolicyEvent{
		Action:    "CleanupSucceeded",
		Message:   "Generated object deleted because conditions are no longer met",
		TargetRef: targetRef,
	})
}

// stampOwnershipLabels sets the admitik ownership labels on the given unstructured object.
func stampOwnershipLabels(obj *unstructured.Unstructured, policyName string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[controller.GeneratedByPolicyLabel] = policyName
	labels[controller.GeneratedByPolicyKind] = controller.ClusterGenerationPolicyResourceType
	obj.SetLabels(labels)
}
