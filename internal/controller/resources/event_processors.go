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
	//"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/common"
	"freepik.com/admitik/internal/globals"
	//"freepik.com/admitik/internal/template"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"
	// TODO
	// "freepik.com/admitik/internal/template"
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

	commonTemplateInjectedObject := map[string]interface{}{}
	commonTemplateInjectedObject["operation"] = eventType
	commonTemplateInjectedObject["object"] = &object[0]
	if eventType == watch.Modified {
		commonTemplateInjectedObject["oldObject"] = &object[0]
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

		logger.Info(fmt.Sprintf("################## CP0 \n"))

		// When some condition is not met, evaluate patch's template and emit a response
		//var kubeEventAction string = "GenerationAborted"
		//var kubeEventMessage string

		//var parsedPatch string
		//var tmpJsonPatchOperations jsondiff.Patch
		//
		//switch policyObj.Spec.Object.Definition.Engine {
		//case v1alpha1.TemplateEngineStarlark:
		//	parsedPatch, err = template.EvaluateTemplateStarlark(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		//default:
		//	parsedPatch, err = template.EvaluateTemplate(policyObj.Spec.Object.Definition.Template, &specificTemplateInjectedObject)
		//}
		//
		//if err != nil {
		//	logger.Info(fmt.Sprintf("failed parsing generation template: %s", err.Error()))
		//	kubeEventMessage = "Generation template failed. More info in controller logs."
		//	goto createKubeEvent
		//}
		//
		//tmpJsonPatchOperations, err = generateJsonPatchOperations(requestObj.Request.Object.Raw, cmPolicyObj.Spec.Patch.Type, []byte(parsedPatch))
		//if err != nil {
		//	logger.Info(fmt.Sprintf("failed generating canonical jsonPatch operations for Kube API server: %s", err.Error()))
		//	kubeEventMessage = "Generated patch is invalid. More info in controller logs."
		//	goto createKubeEvent
		//}
		//
		//jsonPatchOperations = append(jsonPatchOperations, tmpJsonPatchOperations...)
		continue

		//createKubeEvent:
		//	err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
		//		object[0], *policyObj, kubeEventAction, kubeEventMessage)
		//	if err != nil {
		//		logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
		//	}
	}
}
