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

package resources

import (
	"fmt"
	"k8s.io/client-go/dynamic"
	"strings"

	//
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/common"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/template"
)

const (
	ObserverTypeNoop                      = "noop"
	ObserverTypeClusterGenerationPolicies = "clustergenerationpolicies"
)

type ProcessorsFuncMapT map[string]func(string, watch.EventType, ...map[string]interface{})

// initProcessors TODO
// This function is intended to be executed as goroutine
func (r *ResourcesController) initProcessors() {
	logger := log.FromContext(globals.Application.Context)

	resources, err := fetchKubeAvailableResources()
	if err != nil {
		logger.Info(fmt.Sprintf("failed obtaining kubernetes available resources: %v", err.Error()))
	}

	r.kubeAvailableResourceList = resources
}

// getProcessorsFuncMap TODO
func (r *ResourcesController) getProcessorsFuncMap() (funcMap ProcessorsFuncMapT) {
	funcMap = ProcessorsFuncMapT{}

	funcMap[ObserverTypeNoop] = r.processEventNoop
	funcMap[ObserverTypeClusterGenerationPolicies] = r.processEventGeneration

	return funcMap
}

// processEvent TODO:
func (r *ResourcesController) processEvent(resourceType string, eventType watch.EventType, object ...map[string]interface{}) (err error) {
	logger := log.FromContext(globals.Application.Context)

	funcMap := r.getProcessorsFuncMap()
	observers, err := r.Dependencies.ResourcesRegistry.GetObservers(resourceType)
	if err != nil {
		logger.Info(fmt.Sprintf("failed getting informer observers: %v", err.Error()))
	}

	for _, observer := range observers {
		function, functionFound := funcMap[observer]
		if !functionFound {
			continue
		}

		go function(resourceType, eventType, object...)
	}

	return err
}

// processEventNoop TODO:
// This function is intended to be executed as goroutine
func (r *ResourcesController) processEventNoop(resourceType string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context)
	logger = logger.WithValues("processor", ObserverTypeNoop)

	basicData, err := globals.GetObjectBasicData(&object[0])
	if err != nil {
		logger.Info("failed getting basic data from object: %v", err.Error())
	}

	logger.Info(fmt.Sprintf("No-op processor triggered by object: %v", basicData))
}

// processEventGeneration TODO:
// This function is intended to be executed as goroutine
func (r *ResourcesController) processEventGeneration(resourceType string, eventType watch.EventType, object ...map[string]interface{}) {
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
	policyList := r.Dependencies.ClusterGenerationPoliciesRegistry.GetResources(resourceType)
	for _, policyObj := range policyList {

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterGenerationPolicy", policyObj.Name)

		// Retrieve the sources declared per policy
		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject["sources"] = common.FetchPolicySources(policyObj, r.Dependencies.SourcesRegistry)

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
		switch policyObj.Spec.Object.Definition.Engine {
		case v1alpha1.TemplateEngineCel:
			parsedDefinition, err = template.EvaluateAndReplaceCelExpressions(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		case v1alpha1.TemplateEngineStarlark:
			parsedDefinition, err = template.EvaluateTemplateStarlark(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		default:
			parsedDefinition, err = template.EvaluateTemplate(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		}

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
		tmpResource = getResourceFromGvk(r.kubeAvailableResourceList, schema.GroupVersionKind{
			Group:   tmpGroup,
			Version: tmpVersion,
			Kind:    resultObjectBasicData["kind"].(string),
		})

		tmpGvrnn = &v1alpha1.ResourceGroupT{
			GroupVersionResource: metav1.GroupVersionResource{
				Group:    tmpGroup,
				Version:  tmpVersion,
				Resource: tmpResource,
			},
			Name:      resultObjectBasicData["name"].(string),
			Namespace: resultObjectBasicData["namespace"].(string),
		}

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

	updateResource:
		if !policyObj.Spec.OverwriteExisting {
			logger.Info(fmt.Sprintf("failed updating generated object from template result: 'OverwriteExisting' is disabled"))
			kubeEventMessage = "Object update after template failed. More info in controller logs."
			goto createKubeEvent
		}

		_, err = resourceClient.Update(
			globals.Application.Context,
			resultObjConverted,
			metav1.UpdateOptions{},
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
