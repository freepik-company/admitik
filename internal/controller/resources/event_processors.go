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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strings"

	//
	"gopkg.in/yaml.v3"
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

func (r *ResourcesController) GetProcessorsFuncMap() (funcMap ProcessorsFuncMapT) {
	funcMap = ProcessorsFuncMapT{}

	funcMap[ObserverTypeNoop] = r.processEventNoop
	funcMap[ObserverTypeClusterGenerationPolicies] = r.processEventGeneration

	return funcMap
}

// TODO:
func (r *ResourcesController) processEvent(resourceType string, eventType watch.EventType, object ...map[string]interface{}) (err error) {
	logger := log.FromContext(globals.Application.Context)

	funcMap := r.GetProcessorsFuncMap()
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

// TODO:
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

// TODO:
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
		var tmpResource *v1alpha1.ResourceGroupT
		var tmpApiVersionParts []string

		//////////////////////////////////////////////////////////////////////////

		var parsedDefinition string
		switch policyObj.Spec.Object.Definition.Engine {
		case v1alpha1.TemplateEngineCel:
			parsedDefinition, err = template.EvaluateAndReplaceCelExpressions(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		case v1alpha1.TemplateEngineStarlark:
			parsedDefinition, err = template.EvaluateTemplateStarlark(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		default:
			parsedDefinition, err = template.EvaluateTemplate(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		}

		//
		if err != nil {
			logger.Info(fmt.Sprintf("failed parsing generation template: %s", err.Error()))
			kubeEventMessage = "Generation template failed. More info in controller logs."
			goto createKubeEvent
		}

		logger.Info(fmt.Sprintf("Los datos crudos, reinona: \n%v \n", parsedDefinition))

		//
		err = yaml.Unmarshal([]byte(parsedDefinition), &resultObject)
		if err != nil {
			logger.Info(fmt.Sprintf("failed decoding template result. Invalid object: %s", err.Error()))
			kubeEventMessage = "Invalid object after template. More info in controller logs."
			goto createKubeEvent
		}

		//
		resultObjectBasicData, err = globals.GetObjectBasicData(&resultObject)
		if err != nil {
			logger.Info(fmt.Sprintf("failed obtaining metadata from template result. Invalid object: %s", err.Error()))
			kubeEventMessage = "Invalid object after template. More info in controller logs."
			goto createKubeEvent
		}

		// TODO
		// apiVersion, kind, name, namespace
		//var tmpGroup, tmpVersion string
		tmpVersion = resultObjectBasicData["apiVersion"].(string)

		tmpApiVersionParts = strings.Split(resultObjectBasicData["apiVersion"].(string), "/")
		if len(tmpApiVersionParts) == 2 {
			tmpGroup = tmpApiVersionParts[0]
			tmpVersion = tmpApiVersionParts[1]
		}
		tmpResource = &v1alpha1.ResourceGroupT{
			GroupVersionResource: metav1.GroupVersionResource{
				Group:   tmpGroup,
				Version: tmpVersion,
				//Resource: resultObjectBasicData["kind"].(string),
				Resource: "configmaps",
			},
			Name:      resultObjectBasicData["name"].(string),
			Namespace: resultObjectBasicData["namespace"].(string),
		}

		resultObjConverted = &unstructured.Unstructured{
			Object: resultObject,
		}

		logger.Info(fmt.Sprintf("El recursito: %v\n", tmpResource))

		_, err = globals.Application.KubeRawClient.
			Resource(schema.GroupVersionResource(tmpResource.GroupVersionResource)).
			Namespace(tmpResource.Namespace).
			Create(
				globals.Application.Context,
				resultObjConverted,
				metav1.CreateOptions{},
			)

		if err != nil {
			logger.Info(fmt.Sprintf("failed creating generated object from template result: %s", err.Error()))
			kubeEventMessage = "Object creation after template failed. More info in controller logs."
			goto createKubeEvent
		}

		// TODO
		// 2. Comprobar que existe (o no) en Kubernetes
		// 3. Crearlo si no existe, updatearlo si existe

		logger.Info(fmt.Sprintf("Esto es lo que voy a crear: \n %v \n", parsedDefinition))
		continue

	createKubeEvent:
		err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
			object[0], *policyObj, kubeEventAction, kubeEventMessage)
		if err != nil {
			logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
		}
	}
}
