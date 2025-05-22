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
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/common"
	"freepik.com/admitik/internal/globals"
	clusterGenerationPolicyRegistry "freepik.com/admitik/internal/registry/clustergenerationpolicy"
	sourcesRegistry "freepik.com/admitik/internal/registry/sources"
	"freepik.com/admitik/internal/template"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

type GenerationProcessorDependencies struct {
	ClusterGenerationPolicyRegistry *clusterGenerationPolicyRegistry.ClusterGenerationPolicyRegistry
	SourcesRegistry                 *sourcesRegistry.SourcesRegistry

	//
	KubeAvailableResourceList *[]GVKR
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
	logger := log.FromContext(globals.Application.Context)
	logger = logger.WithValues("processor", ObserverTypeClusterGenerationPolicies)

	var err error

	commonTemplateInjectedObject := map[string]interface{}{}
	commonTemplateInjectedObject["operation"] = string(eventType)
	commonTemplateInjectedObject["object"] = &object[0]
	if eventType == watch.Modified {
		commonTemplateInjectedObject["oldObject"] = &object[1]
	}

	//
	policyList := p.dependencies.ClusterGenerationPolicyRegistry.GetResources(resourceType)
	for _, policyObj := range policyList {

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterGenerationPolicy", policyObj.Name)

		// Retrieve the sources declared per policy
		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject["sources"] = common.FetchPolicySources(policyObj, p.dependencies.SourcesRegistry)

		//Evaluate template conditions
		conditionsPassed, condErr := common.IsPassingConditions(policyObj.Spec.Conditions, &specificTemplateInjectedObject)
		if condErr != nil {
			logger.Info(fmt.Sprintf("failed evaluating conditions: %s", condErr.Error()))
		}

		// Conditions are not met, skip generating the resource
		if !conditionsPassed {
			continue
		}

		// When conditions are met, evaluate generation's template and emit a response
		var kubeEventAction string = "GenerationAborted"
		var kubeEventMessage string

		// FIXME: Arrange everything to avoid declaring no-sense vars here
		//////////////////////////////////////////////////////////////////////////
		var resultObject map[string]any
		var resultObjectBasicData map[string]any

		var tmpGroup, tmpVersion string
		var resultObjConverted *unstructured.Unstructured
		var tmpResource string
		var tmpGvrnn *v1alpha1.ResourceGroupT
		var tmpApiVersionParts []string
		var resourceClient dynamic.ResourceInterface
		//////////////////////////////////////////////////////////////////////////

		// Evaluate template for generating the resource
		var parsedDefinition string
		parsedDefinition, err = template.EvaluateTemplate(policyObj.Spec.Object.Definition.Engine,
			policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)

		if err != nil {
			logger.Info(fmt.Sprintf("failed parsing generation template: %s", err.Error()))
			kubeEventMessage = "Generation template failed. More info in controller logs."
			goto createKubeEvent
		}

		// Check the result to know whether is finally a YAML or not
		err = yaml.Unmarshal([]byte(parsedDefinition), &resultObject)
		if err != nil {
			logger.Info(fmt.Sprintf("failed decoding template result. Invalid object: %s", err.Error()))
			kubeEventMessage = "Invalid object after template. More info in controller logs."
			goto createKubeEvent
		}

		// Result MUST be valid, so try to extract some basic data to check
		resultObjectBasicData, err = globals.GetObjectBasicData(&resultObject)
		if err != nil {
			logger.Info(fmt.Sprintf("failed obtaining metadata from template result. Invalid object: %s", err.Error()))
			kubeEventMessage = "Invalid object after template. More info in controller logs."
			goto createKubeEvent
		}

		// Craft a metadata object for client-go to perform actions
		tmpVersion = resultObjectBasicData["apiVersion"].(string)
		tmpApiVersionParts = strings.Split(tmpVersion, "/")
		if len(tmpApiVersionParts) == 2 {
			tmpGroup = tmpApiVersionParts[0]
			tmpVersion = tmpApiVersionParts[1]
		}

		tmpResource = getResourceFromGvk(p.dependencies.KubeAvailableResourceList, schema.GroupVersionKind{
			Group:   tmpGroup,
			Version: tmpVersion,
			Kind:    resultObjectBasicData["kind"].(string),
		})

		if tmpResource == "" {
			logger.Info("failed obtaining resource equivalent from Kubernetes for provided GVK. Is this resource defined?")
			kubeEventMessage = "Unknown object resource for provided GVK. More info in controller logs."
			goto createKubeEvent
		}

		tmpGvrnn = &v1alpha1.ResourceGroupT{
			GroupVersionResource: metav1.GroupVersionResource{
				Group:    tmpGroup,
				Version:  tmpVersion,
				Resource: tmpResource,
			},
			Name:      resultObjectBasicData["name"].(string),
			Namespace: resultObjectBasicData["namespace"].(string),
		}
		logger.WithValues(
			"group", tmpGvrnn.Group,
			"version", tmpGvrnn.Version,
			"resource", tmpGvrnn.Resource,
			"name", resultObjectBasicData["name"].(string),
			"namespace", resultObjectBasicData["namespace"].(string))

		resultObjConverted = &unstructured.Unstructured{
			Object: resultObject,
		}

		// Perform actions against Kubernetes
		resourceClient = globals.Application.KubeRawClient.
			Resource(schema.GroupVersionResource(tmpGvrnn.GroupVersionResource)).
			Namespace(tmpGvrnn.Namespace)

		_, err = resourceClient.Create(
			globals.Application.Context,
			resultObjConverted,
			metav1.CreateOptions{},
		)

		if err != nil {
			if !errors.IsAlreadyExists(err) {
				logger.Info(fmt.Sprintf("failed creating generated object from template result: %s", err.Error()))
				kubeEventMessage = "Object creation after template failed. More info in controller logs."
				goto createKubeEvent
			}

			goto updateResource
		}

		continue

	updateResource:
		if !policyObj.Spec.OverwriteExisting {
			logger.Info(fmt.Sprintf("failed updating generated object from template result: 'OverwriteExisting' is disabled"))
			kubeEventMessage = "Object update after template failed. More info in controller logs."
			goto createKubeEvent
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
			kubeEventMessage = "Object update after template failed. More info in controller logs."
			goto createKubeEvent
		}
		continue

	createKubeEvent:
		err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
			object[0], *policyObj, kubeEventAction, kubeEventMessage)
		if err != nil {
			logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
		}
	}
}
