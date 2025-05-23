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

package common

import (
	"context"
	"fmt"
	"time"

	//
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/registry/sources"
	"freepik.com/admitik/internal/template"
)

// IsPassingConditions iterate over a list of templated conditions and return whether they are passing or not
func IsPassingConditions(conditionList []v1alpha1.ConditionT, injectedData *template.InjectedDataT) (result bool, err error) {
	for _, condition := range conditionList {

		// Choose templating engine. Maybe more will be added in the future
		parsedKey, condErr := template.EvaluateTemplate(condition.Engine, condition.Key, injectedData)
		if condErr != nil {
			return false, fmt.Errorf("failed condition '%s': %s", condition.Name, condErr.Error())
		}

		if parsedKey != condition.Value {
			return false, nil
		}
	}

	return true, nil
}

// FetchPolicySources TODO
func FetchPolicySources(policyObj any, sourcesReg *sources.SourcesRegistry) (results map[int][]map[string]any) {

	results = make(map[int][]map[string]any)

	var policySources []v1alpha1.ResourceGroupT

	switch p := (policyObj).(type) {
	case *v1alpha1.ClusterGenerationPolicy:
		policySources = p.Spec.Sources
	case *v1alpha1.ClusterValidationPolicy:
		policySources = p.Spec.Sources
	case *v1alpha1.ClusterMutationPolicy:
		policySources = p.Spec.Sources
	default:
		return results
	}

	for sourceIndex, sourceItem := range policySources {

		sourceString := fmt.Sprintf("%s/%s/%s/%s/%s",
			sourceItem.Group,
			sourceItem.Version,
			sourceItem.Resource,
			sourceItem.Namespace,
			sourceItem.Name)

		sourceObjList := sourcesReg.GetResources(sourceString)

		// Store obtained sources in the results map
		for _, itemObj := range sourceObjList {
			results[sourceIndex] = append(results[sourceIndex], *itemObj)
		}
	}

	return results
}

// CreateKubeEvent creates a modern event in Kubernetes with data given by params
func CreateKubeEvent(ctx context.Context, namespace string, reporter string, object map[string]interface{},
	policyObj any, action, message string) error {

	objectData, err := globals.GetObjectBasicData(&object)
	if err != nil {
		return err
	}

	var eventReason string
	var policyApiVersion, policyKind, policyName string

	switch p := policyObj.(type) {
	case v1alpha1.ClusterValidationPolicy:
		policyApiVersion = p.APIVersion
		policyKind = p.Kind
		policyName = p.Name
		eventReason = "ClusterValidationPolicyAudit"

	case v1alpha1.ClusterMutationPolicy:
		policyApiVersion = p.APIVersion
		policyKind = p.Kind
		policyName = p.Name
		eventReason = "ClusterMutationPolicyAudit"

	case v1alpha1.ClusterGenerationPolicy:
		policyApiVersion = p.APIVersion
		policyKind = p.Kind
		policyName = p.Name
		eventReason = "ClusterGenerationPolicyAudit"

	default:
		return fmt.Errorf("unsupported policy type")
	}

	eventObj := eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reporter + "-",
		},

		EventTime:           metav1.NewMicroTime(time.Now()),
		ReportingController: "admitik",
		ReportingInstance:   reporter,
		Action:              action,
		Reason:              eventReason,

		Regarding: corev1.ObjectReference{
			APIVersion: objectData["apiVersion"].(string),
			Kind:       objectData["kind"].(string),
			Name:       objectData["name"].(string),
			Namespace:  objectData["namespace"].(string),
		},

		Related: &corev1.ObjectReference{
			APIVersion: policyApiVersion,
			Kind:       policyKind,
			Name:       policyName,
		},

		Note: message,
		Type: "Normal",
	}

	_, err = globals.Application.KubeRawCoreClient.EventsV1().Events(namespace).
		Create(ctx, &eventObj, metav1.CreateOptions{})

	return err
}
