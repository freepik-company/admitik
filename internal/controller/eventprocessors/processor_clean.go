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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/globals"
	informerRegistry "github.com/freepik-company/admitik/internal/registry/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/template"
)

// CleanProcessorDependencies holds the external dependencies required by CleanProcessor.
type CleanProcessorDependencies struct {
	ClusterCleanPolicyRegistry  *policyStore.PolicyStore[*v1alpha1.ClusterCleanPolicy]
	SourcesPool                 informerRegistry.SourcesPool
	KubeAvailableResourceListFn func() []GVKR
}

// CleanProcessor handles events for watched resources and deletes target resources
// when conditions are met, according to each matching ClusterCleanPolicy.
type CleanProcessor struct {
	dependencies CleanProcessorDependencies
}

// NewCleanProcessor creates a CleanProcessor wired to the given dependencies.
func NewCleanProcessor(deps CleanProcessorDependencies) *CleanProcessor {
	return &CleanProcessor{dependencies: deps}
}

// Process is the entry point called by WatchedEventListener when a watched resource event arrives.
// For each matching ClusterCleanPolicy it evaluates conditions and, when they pass, deletes
// the templated target resource.
func (p *CleanProcessor) Process(resourceType string, eventType watch.EventType, objects ...map[string]interface{}) {
	baseData := buildEventContext(eventType, objects)
	logger := newProcessorLogger(ObserverTypeClusterCleanPolicies, objects[0], baseData.Operation)
	emitter := newEventEmitter(logger)

	deps := commonDeps{
		SourcesPool:             p.dependencies.SourcesPool,
		KubeAvailableResourceFn: p.dependencies.KubeAvailableResourceListFn,
	}

	for _, policy := range p.dependencies.ClusterCleanPolicyRegistry.GetResources(resourceType) {
		policyLogger := logger.WithValues("ClusterCleanPolicy", policy.Name)

		passed, evalData, err := evaluatePolicy(policy, &baseData, deps, policyLogger, emitter, objects[0])
		if err != nil {
			continue
		}

		if !passed {
			policyLogger.V(1).Info("conditions not met, skipping clean")
			continue
		}

		p.processClean(policy, evalData, policyLogger, emitter, objects[0])
	}
}

// processClean renders the target template, resolves the GVR, and deletes the resource.
func (p *CleanProcessor) processClean(
	policy *v1alpha1.ClusterCleanPolicy,
	data *template.PolicyEvaluationDataT,
	logger logr.Logger,
	emitter *common.EventEmitter,
	triggerObj map[string]interface{},
) {
	_, bd, gvr, errMsg := renderAndResolve(
		policy.Spec.Target.Engine,
		policy.Spec.Target.Template,
		data, p.dependencies.KubeAvailableResourceListFn(), logger,
	)
	if errMsg != "" {
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:  "CleanAborted",
			Message: errMsg,
		})
		return
	}

	targetRef := common.TargetRefFromBasicData(bd)

	logger = logger.WithValues(
		"group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource,
		"name", bd.Name, "namespace", bd.Namespace,
	)

	client := globals.Application.KubeRawClient.Resource(gvr).Namespace(bd.Namespace)

	if err := client.Delete(globals.Application.Context, bd.Name, metav1.DeleteOptions{}); err != nil {
		logger.Info("failed deleting target resource", "error", err.Error())
		emitter.Emit(triggerObj, policy, common.PolicyEvent{
			Action:    "CleanAborted",
			Message:   fmt.Sprintf("Object deletion failed: %s", err.Error()),
			TargetRef: targetRef,
		})
		return
	}

	emitter.Emit(triggerObj, policy, common.PolicyEvent{
		Action:    "CleanSucceeded",
		Message:   "Target resource deleted successfully",
		TargetRef: targetRef,
	})
}
