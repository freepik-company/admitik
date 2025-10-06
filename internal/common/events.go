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
	"strings"
	"time"

	//
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/globals"
)

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
			APIVersion: strings.Join([]string{objectData.Group, objectData.Version}, "/"),
			Kind:       objectData.Kind,
			Name:       objectData.Name,
			Namespace:  objectData.Namespace,
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
