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

package observedresource

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
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	"github.com/freepik-company/admitik/internal/template"
)

type GenerationProcessorDependencies struct {
	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	SourcesPool                     informerRegistry.SourcesPool

	KubeAvailableResourceListFn func() []GVKR
}

type GenerationProcessor struct {
	dependencies GenerationProcessorDependencies
}

func NewGenerationProcessor(deps GenerationProcessorDependencies) *GenerationProcessor {
	return &GenerationProcessor{
		dependencies: deps,
	}
}

func (p *GenerationProcessor) Process(resourceType string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context).WithValues("processor", ObserverTypeClusterGenerationPolicies)

	var err error

	// Create an object that will be injected in conditions/message
	// in later template evaluation stage
	commonTemplateInjectedObject := template.PolicyEvaluationDataT{}
	commonTemplateInjectedObject.Initialize()

	commonTemplateInjectedObject.Operation = common.GetNormalizedOperation(eventType)
	commonTemplateInjectedObject.Object = object[0]

	if commonTemplateInjectedObject.Operation == common.NormalizedOperationUpdate {
		commonTemplateInjectedObject.OldObject = object[1]
	}

	//
	policyList := p.dependencies.ClusterGenerationPolicyRegistry.GetResources(resourceType)
	for _, policyObj := range policyList {

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterGenerationPolicy", policyObj.Name)

		// Retrieve the sources declared per policy
		triggerInjectedObject := commonTemplateInjectedObject.TriggerInjectedDataT
		tmpFetchedPolicySources, fetchErr := common.FetchPolicySources(p.dependencies.SourcesPool, policyObj, &triggerInjectedObject)
		if fetchErr != nil {
			logger.Info("failed fetching sources. Broken ones will be empty", "error", fetchErr.Error())
		}

		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject.Sources = tmpFetchedPolicySources

		//Evaluate template conditions
		conditionsPassed, condErr := common.IsPassingConditions(policyObj.Spec.Conditions, &specificTemplateInjectedObject)
		if condErr != nil {
			logger.Info(fmt.Sprintf("failed evaluating conditions: %s", condErr.Error()))
		}

		// Conditions are not met, skip generating the resource
		if !conditionsPassed {
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

// processGeneration evaluates the generation template and creates/updates the resource.
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
