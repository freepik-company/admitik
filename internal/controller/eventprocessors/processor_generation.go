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
	"gopkg.in/yaml.v3"

	//
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
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

	// KubeAvailableResourceListFn returns the current cached list of Kubernetes API resources.
	// Used to resolve a GVK to its resource name when creating or deleting objects.
	KubeAvailableResourceListFn func() []GVKR
}

// GenerationProcessor handles events for watched resources and creates, updates, or deletes
// generated objects according to each matching ClusterGenerationPolicy.
type GenerationProcessor struct {
	dependencies GenerationProcessorDependencies
}

// NewGenerationProcessor creates a GenerationProcessor wired to the given dependencies.
func NewGenerationProcessor(deps GenerationProcessorDependencies) *GenerationProcessor {
	return &GenerationProcessor{
		dependencies: deps,
	}
}

// Process is the entry point called by WatchedEventListener when a watched resource event arrives.
// For each matching ClusterGenerationPolicy it:
//   - evaluates conditions against the event object and sources,
//   - creates/updates the generated object when conditions pass,
//   - deletes the generated object when conditions fail and DeleteOnConditionFalse is enabled.
func (p *GenerationProcessor) Process(resourceType string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context).WithValues("processor", ObserverTypeClusterGenerationPolicies)

	var err error

	commonTemplateInjectedObject := template.PolicyEvaluationDataT{}
	commonTemplateInjectedObject.Initialize()

	commonTemplateInjectedObject.Operation = common.GetNormalizedOperation(eventType)
	commonTemplateInjectedObject.Object = object[0]

	if commonTemplateInjectedObject.Operation == common.NormalizedOperationUpdate && len(object) > 1 {
		commonTemplateInjectedObject.OldObject = object[1]
	}

	if triggerBasicData, bdErr := globals.GetObjectBasicData(&object[0]); bdErr == nil {
		logger = logger.WithValues(
			"triggerGroup", triggerBasicData.Group,
			"triggerVersion", triggerBasicData.Version,
			"triggerKind", triggerBasicData.Kind,
			"triggerName", triggerBasicData.Name,
			"triggerNamespace", triggerBasicData.Namespace,
			"triggerOperation", commonTemplateInjectedObject.Operation,
		)
	}

	policyList := p.dependencies.ClusterGenerationPolicyRegistry.GetResources(resourceType)
	for _, policyObj := range policyList {

		logger = logger.WithValues("ClusterGenerationPolicy", policyObj.Name)

		triggerInjectedObject := commonTemplateInjectedObject.TriggerInjectedDataT
		tmpFetchedPolicySources, fetchErr := common.FetchPolicySources(p.dependencies.SourcesPool, policyObj, &triggerInjectedObject)
		if fetchErr != nil {
			logger.Info("failed fetching sources. Broken ones will be empty", "error", fetchErr.Error())
		}

		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject.Sources = tmpFetchedPolicySources

		conditionsPassed, condErr := common.IsPassingConditions(policyObj.Spec.Conditions, &specificTemplateInjectedObject)
		if condErr != nil {
			logger.V(1).Info(fmt.Sprintf("failed evaluating conditions: %s", condErr.Error()))
			err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
				object[0], *policyObj, "ConditionEvaluationFailed", condErr.Error())
			if err != nil {
				logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
			}
			continue
		}

		if !conditionsPassed {
			logger.V(1).Info("conditions not met, skipping generation")
			// When conditions are not met and the policy opts into cleanup, delete any
			// previously generated object that matches this policy's template output.
			if policyObj.Spec.DeleteOnConditionFalse {
				if eventMsg := p.processCleanup(policyObj, &specificTemplateInjectedObject, logger); eventMsg != "" {
					err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
						object[0], *policyObj, "CleanupAborted", eventMsg)
					if err != nil {
						logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
					}
				}
			}
			continue
		}

		if eventMessage := p.processGeneration(policyObj, &specificTemplateInjectedObject, logger); eventMessage != "" {
			err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
				object[0], *policyObj, "GenerationAborted", eventMessage)
			if err != nil {
				logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
			}
		}
	}
}

// processGeneration evaluates the generation template and creates or updates the resource.
// Returns an event message if something went wrong, or empty string on success.
func (p *GenerationProcessor) processGeneration(policyObj *v1alpha1.ClusterGenerationPolicy, injectedData *template.PolicyEvaluationDataT, logger logr.Logger) string {

	parsedDefinition, err := template.EvaluateTemplate(policyObj.Spec.Object.Definition.Engine,
		policyObj.Spec.Object.Definition.Template, injectedData)
	if err != nil {
		logger.Info(fmt.Sprintf("failed parsing generation template: %s", err.Error()))
		return "Generation template failed. More info in controller logs."
	}

	var resultObject map[string]any
	if err = yaml.Unmarshal([]byte(parsedDefinition), &resultObject); err != nil {
		logger.Info(fmt.Sprintf("failed decoding template result. Invalid object: %s", err.Error()))
		return "Invalid object after template. More info in controller logs."
	}

	resultObjectBasicData, err := globals.GetObjectBasicData(&resultObject)
	if err != nil {
		logger.Info(fmt.Sprintf("failed obtaining metadata from template result. Invalid object: %s", err.Error()))
		return "Invalid object after template. More info in controller logs."
	}

	kubeResources := p.dependencies.KubeAvailableResourceListFn()
	tmpResource := getResourceFromGvk(kubeResources, schema.GroupVersionKind{
		Group:   resultObjectBasicData.Group,
		Version: resultObjectBasicData.Version,
		Kind:    resultObjectBasicData.Kind,
	})
	if tmpResource == "" {
		logger.Info("failed obtaining resource equivalent from Kubernetes for provided GVK. Is this resource defined?")
		return "Unknown object resource for provided GVK. More info in controller logs."
	}

	gvr := schema.GroupVersionResource{
		Group:    resultObjectBasicData.Group,
		Version:  resultObjectBasicData.Version,
		Resource: tmpResource,
	}
	logger = logger.WithValues(
		"group", gvr.Group,
		"version", gvr.Version,
		"resource", gvr.Resource,
		"name", resultObjectBasicData.Name,
		"namespace", resultObjectBasicData.Namespace)

	resultObjConverted := &unstructured.Unstructured{
		Object: resultObject,
	}

	existingLabels := resultObjConverted.GetLabels()
	if existingLabels == nil {
		existingLabels = map[string]string{}
	}
	existingLabels[controller.GeneratedByPolicyLabel] = policyObj.Name
	existingLabels[controller.GeneratedByPolicyKind] = controller.ClusterGenerationPolicyResourceType
	resultObjConverted.SetLabels(existingLabels)

	resourceClient := globals.Application.KubeRawClient.
		Resource(gvr).
		Namespace(resultObjectBasicData.Namespace)

	_, err = resourceClient.Create(
		globals.Application.Context,
		resultObjConverted,
		metav1.CreateOptions{},
	)
	if err == nil {
		return ""
	}

	if !errors.IsAlreadyExists(err) {
		logger.Info(fmt.Sprintf("failed creating generated object from template result: %s", err.Error()))
		return "Object creation after template failed. More info in controller logs."
	}

	if !policyObj.Spec.OverwriteExisting {
		logger.Info("failed updating generated object from template result: 'OverwriteExisting' is disabled")
		return "Object update after template failed. More info in controller logs."
	}

	_, err = resourceClient.Apply(
		globals.Application.Context,
		resultObjConverted.GetName(),
		resultObjConverted,
		metav1.ApplyOptions{
			FieldManager: controllerName,
			Force:        true,
		},
	)
	if err != nil {
		logger.Info(fmt.Sprintf("failed updating generated object from template result: %s", err.Error()))
		return "Object update after template failed. More info in controller logs."
	}

	return ""
}

// processCleanup renders the generation template to resolve the target object's identity
// (GVK, name, namespace) and deletes it if it exists and carries the policy ownership labels.
// This is called when conditions evaluate to false and DeleteOnConditionFalse is enabled.
// Returns an event message if something went wrong, or empty string on success (including
// the case where the object simply does not exist).
func (p *GenerationProcessor) processCleanup(policyObj *v1alpha1.ClusterGenerationPolicy, injectedData *template.PolicyEvaluationDataT, logger logr.Logger) string {

	parsedDefinition, err := template.EvaluateTemplate(policyObj.Spec.Object.Definition.Engine,
		policyObj.Spec.Object.Definition.Template, injectedData)
	if err != nil {
		logger.Info(fmt.Sprintf("failed parsing generation template for cleanup: %s", err.Error()))
		return "Cleanup template failed. More info in controller logs."
	}

	var resultObject map[string]any
	if err = yaml.Unmarshal([]byte(parsedDefinition), &resultObject); err != nil {
		logger.Info(fmt.Sprintf("failed decoding template result for cleanup. Invalid object: %s", err.Error()))
		return "Invalid object after cleanup template. More info in controller logs."
	}

	resultObjectBasicData, err := globals.GetObjectBasicData(&resultObject)
	if err != nil {
		logger.Info(fmt.Sprintf("failed obtaining metadata from template result for cleanup: %s", err.Error()))
		return "Invalid object after cleanup template. More info in controller logs."
	}

	kubeResources := p.dependencies.KubeAvailableResourceListFn()
	tmpResource := getResourceFromGvk(kubeResources, schema.GroupVersionKind{
		Group:   resultObjectBasicData.Group,
		Version: resultObjectBasicData.Version,
		Kind:    resultObjectBasicData.Kind,
	})
	if tmpResource == "" {
		logger.Info("failed obtaining resource equivalent from Kubernetes for provided GVK during cleanup")
		return "Unknown object resource for provided GVK. More info in controller logs."
	}

	gvr := schema.GroupVersionResource{
		Group:    resultObjectBasicData.Group,
		Version:  resultObjectBasicData.Version,
		Resource: tmpResource,
	}

	resourceClient := globals.Application.KubeRawClient.
		Resource(gvr).
		Namespace(resultObjectBasicData.Namespace)

	existing, err := resourceClient.Get(globals.Application.Context, resultObjectBasicData.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return ""
		}
		logger.Info(fmt.Sprintf("failed getting existing object for cleanup: %s", err.Error()))
		return "Cleanup get failed. More info in controller logs."
	}

	// Only delete objects that were created by this policy to avoid touching unrelated resources.
	labels := existing.GetLabels()
	if labels[controller.GeneratedByPolicyLabel] != policyObj.Name ||
		labels[controller.GeneratedByPolicyKind] != controller.ClusterGenerationPolicyResourceType {
		return ""
	}

	if err = resourceClient.Delete(globals.Application.Context, resultObjectBasicData.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("failed deleting generated object during cleanup: %s", err.Error()))
		return "Object deletion during cleanup failed. More info in controller logs."
	}

	return ""
}
