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

	"github.com/go-logr/logr"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/globals"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/template"
)

type CleanProcessorDependencies struct {
	ClusterCleanPolicyRegistry  *policyStore.PolicyStore[*v1alpha1.ClusterCleanPolicy]
	SourcesPool                 informerRegistry.SourcesPool
	KubeAvailableResourceListFn func() []GVKR
}

type CleanProcessor struct {
	dependencies CleanProcessorDependencies
}

func NewCleanProcessor(deps CleanProcessorDependencies) *CleanProcessor {
	return &CleanProcessor{
		dependencies: deps,
	}
}

func (p *CleanProcessor) Process(resourceType string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context).WithValues("processor", ObserverTypeClusterCleanPolicies)

	var err error

	commonTemplateInjectedObject := template.PolicyEvaluationDataT{}
	commonTemplateInjectedObject.Initialize()

	commonTemplateInjectedObject.Operation = common.GetNormalizedOperation(eventType)
	commonTemplateInjectedObject.Object = object[0]

	if commonTemplateInjectedObject.Operation == common.NormalizedOperationUpdate {
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

	policyList := p.dependencies.ClusterCleanPolicyRegistry.GetResources(resourceType)
	for _, policyObj := range policyList {

		logger = logger.WithValues("ClusterCleanPolicy", policyObj.Name)

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
			logger.V(1).Info("conditions not met, skipping clean")
			continue
		}

		if eventMessage := p.processClean(policyObj, &specificTemplateInjectedObject, logger); eventMessage != "" {
			err = common.CreateKubeEvent(globals.Application.Context, "default", "resources-controller",
				object[0], *policyObj, "CleanAborted", eventMessage)
			if err != nil {
				logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
			}
		}
	}
}

// processClean evaluates the clean target template and deletes the target resource.
// Returns an event message if something went wrong, or empty string on success.
func (p *CleanProcessor) processClean(policyObj *v1alpha1.ClusterCleanPolicy, injectedData *template.PolicyEvaluationDataT, logger logr.Logger) string {

	parsedTarget, err := template.EvaluateTemplate(policyObj.Spec.Target.Engine,
		policyObj.Spec.Target.Template, injectedData)
	if err != nil {
		logger.Info(fmt.Sprintf("failed parsing clean target template: %s", err.Error()))
		return "Clean target template failed. More info in controller logs."
	}

	var targetDefinition map[string]any
	if err = yaml.Unmarshal([]byte(parsedTarget), &targetDefinition); err != nil {
		logger.Info(fmt.Sprintf("failed decoding target template result. Invalid object: %s", err.Error()))
		return "Invalid target object after template. More info in controller logs."
	}

	targetBasicData, err := globals.GetObjectBasicData(&targetDefinition)
	if err != nil {
		logger.Info(fmt.Sprintf("failed obtaining metadata from target template result. Invalid object: %s", err.Error()))
		return "Invalid target object after template. More info in controller logs."
	}

	kubeResources := p.dependencies.KubeAvailableResourceListFn()
	tmpResource := getResourceFromGvk(kubeResources, schema.GroupVersionKind{
		Group:   targetBasicData.Group,
		Version: targetBasicData.Version,
		Kind:    targetBasicData.Kind,
	})
	if tmpResource == "" {
		logger.Info("failed obtaining resource equivalent from Kubernetes for provided GVK. Is this resource defined?")
		return "Unknown object resource for provided GVK. More info in controller logs."
	}

	logger = logger.WithValues(
		"group", targetBasicData.Group,
		"version", targetBasicData.Version,
		"resource", tmpResource,
		"name", targetBasicData.Name,
		"namespace", targetBasicData.Namespace)

	resourceClient := globals.Application.KubeRawClient.
		Resource(schema.GroupVersionResource{
			Group:    targetBasicData.Group,
			Version:  targetBasicData.Version,
			Resource: tmpResource,
		}).
		Namespace(targetBasicData.Namespace)

	err = resourceClient.Delete(
		globals.Application.Context,
		targetBasicData.Name,
		metav1.DeleteOptions{},
	)
	if err != nil {
		logger.Info(fmt.Sprintf("failed deleting target resource: %s", err.Error()))
		return "Object deletion failed. More info in controller logs."
	}

	logger.Info("target resource deleted successfully")
	return ""
}
