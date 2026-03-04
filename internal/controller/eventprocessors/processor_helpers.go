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
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/globals"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	"github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/template"
)

// commonDeps groups the dependencies shared by all background event processors.
type commonDeps struct {
	SourcesPool             informerRegistry.SourcesPool
	KubeAvailableResourceFn func() []GVKR
}

// newProcessorLogger creates a base logger tagged with the processor name and enriched
// with the trigger object's identity (group, version, kind, name, namespace, operation).
func newProcessorLogger(processorName string, triggerObj map[string]interface{}, operation string) logr.Logger {
	logger := log.FromContext(globals.Application.Context).WithValues("processor", processorName)

	bd, err := globals.GetObjectBasicData(&triggerObj)
	if err != nil {
		return logger
	}

	return logger.WithValues(
		"triggerGroup", bd.Group,
		"triggerVersion", bd.Version,
		"triggerKind", bd.Kind,
		"triggerName", bd.Name,
		"triggerNamespace", bd.Namespace,
		"triggerOperation", operation,
	)
}

// newEventEmitter creates an EventEmitter for background processors, bound to
// the application context and the given logger.
func newEventEmitter(logger logr.Logger) *common.EventEmitter {
	return common.NewEventEmitter(globals.Application.Context, "resources-controller", logger)
}

// buildEventContext creates a PolicyEvaluationDataT pre-populated with the trigger
// operation, current object, and (for updates) the previous object revision.
func buildEventContext(eventType watch.EventType, objects []map[string]interface{}) template.PolicyEvaluationDataT {
	data := template.PolicyEvaluationDataT{}
	data.Initialize()

	data.Operation = common.GetNormalizedOperation(eventType)
	data.Object = objects[0]

	if data.Operation == common.NormalizedOperationUpdate && len(objects) > 1 {
		data.OldObject = objects[1]
	}

	return data
}

// evaluatePolicy fetches sources, evaluates the policy conditions and returns whether
// they passed together with the fully-populated evaluation data for downstream actions.
// On condition evaluation error it emits a Kubernetes event and returns a non-nil error.
func evaluatePolicy[T policystore.PolicyResourceI](
	policy T,
	baseData *template.PolicyEvaluationDataT,
	deps commonDeps,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) (passed bool, evalData *template.PolicyEvaluationDataT, err error) {

	triggerInjected := baseData.TriggerInjectedDataT
	sources, fetchErr := common.FetchPolicySources(deps.SourcesPool, policy, &triggerInjected)
	if fetchErr != nil {
		logger.Info("failed fetching sources, broken ones will be empty", "error", fetchErr.Error())
	}

	data := *baseData
	data.Sources = sources
	evalData = &data

	passed, condErr := common.IsPassingConditions(policy.GetConditions(), evalData)
	if condErr != nil {
		logger.V(1).Info("failed evaluating conditions", "error", condErr.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:  "ConditionEvaluationFailed",
			Message: condErr.Error(),
		})
		return false, evalData, condErr
	}

	return passed, evalData, nil
}

// renderAndResolve renders a template engine/template pair, unmarshals the YAML result,
// extracts object basic data and resolves the Kubernetes resource name for the resulting GVK.
// Returns the unmarshaled object map, its metadata, and the GVR — or a non-empty error
// message with the real error detail.
func renderAndResolve(
	engine, tmpl string,
	data *template.PolicyEvaluationDataT,
	kubeResources []GVKR,
	logger logr.Logger,
) (obj map[string]any, bd globals.ObjectBasicData, gvr schema.GroupVersionResource, errMsg string) {

	rendered, err := template.EvaluateTemplate(engine, tmpl, data)
	if err != nil {
		logger.Info("failed rendering template", "error", err.Error())
		return nil, bd, gvr, fmt.Sprintf("Template rendering failed: %s", err.Error())
	}

	if err = yaml.Unmarshal([]byte(rendered), &obj); err != nil {
		logger.Info("failed decoding template result as YAML", "error", err.Error())
		return nil, bd, gvr, fmt.Sprintf("Invalid YAML after template: %s", err.Error())
	}

	bd, err = globals.GetObjectBasicData(&obj)
	if err != nil {
		logger.Info("failed extracting metadata from template result", "error", err.Error())
		return nil, bd, gvr, fmt.Sprintf("Invalid object metadata after template: %s", err.Error())
	}

	resource := getResourceFromGvk(kubeResources, schema.GroupVersionKind{
		Group: bd.Group, Version: bd.Version, Kind: bd.Kind,
	})
	if resource == "" {
		logger.Info("unknown Kubernetes resource for GVK — is this resource defined?")
		return nil, bd, gvr, fmt.Sprintf("Unknown resource for GVK %s/%s %s", bd.Group, bd.Version, bd.Kind)
	}

	gvr = schema.GroupVersionResource{Group: bd.Group, Version: bd.Version, Resource: resource}
	return obj, bd, gvr, ""
}
