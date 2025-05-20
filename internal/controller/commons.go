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

package controller

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
)

const (
	ClusterValidationPolicyResourceType = "ClusterValidationPolicy"
	ClusterMutationPolicyResourceType   = "ClusterMutationPolicy"
	ClusterGenerationPolicyResourceType = "ClusterGenerationPolicy"

	//
	ResourceNotFoundError         = "%s '%s' resource not found. Ignoring since object must be deleted."
	ResourceRetrievalError        = "Error getting the %s '%s' from the cluster: %s"
	ResourceFinalizersUpdateError = "Failed to update finalizer of %s '%s': %s"
	ResourceConditionUpdateError  = "Failed to update the condition on %s '%s': %s"
	ResourceReconcileError        = "Can not reconcile %s '%s': %s"

	//
	ResourceFinalizer = "admitik.freepik.com/finalizer"
)

// GetWebhookClientConfig return a WebhookClientConfig filled according to if the remote server
// is truly remote or inside Kubernetes
func GetWebhookClientConfig(CABundle []byte, serverHostname string, serverPort int, serverPath string) (wcConfig admissionregv1.WebhookClientConfig, err error) {

	wcConfig.CABundle = CABundle

	if strings.HasSuffix(serverHostname, ".svc") || strings.HasSuffix(serverHostname, ".svc.cluster.local") {
		tmpWebhooksClientHostname := strings.TrimSuffix(serverHostname, ".svc")
		tmpWebhooksClientHostname = strings.TrimSuffix(tmpWebhooksClientHostname, ".svc.cluster.local")

		hostnameParts := strings.Split(tmpWebhooksClientHostname, ".")
		if len(hostnameParts) != 2 {
			return wcConfig, fmt.Errorf("invalid hostname for internal Kubernetes service. It must match 'x.x.svc' or 'y.y.svc.cluster.local'")
		}

		webhooksClientPortConv := int32(serverPort)
		wcConfig.Service = &admissionregv1.ServiceReference{
			Name:      hostnameParts[0],
			Namespace: hostnameParts[1],
			Port:      &webhooksClientPortConv,
			Path:      &serverPath,
		}
	} else {

		webhooksClientHost := fmt.Sprintf("%s:%d", serverHostname, serverPort)
		webhooksServerUrl := url.URL{
			Scheme: "https",
			Host:   webhooksClientHost,
			Path:   serverPath,
		}

		webhooksServerUrlString := webhooksServerUrl.String()
		wcConfig.URL = &webhooksServerUrlString
	}

	return wcConfig, err
}

// GetSpecificWebhookClientConfigs receive a webhookClientConfig object, returning two copies with paths adapted
// for validation and mutation admission endpoints
func GetSpecificWebhookClientConfigs(wcConfig admissionregv1.WebhookClientConfig, validationPath, mutationPath string) (validationWcConfig, mutationWcConfig admissionregv1.WebhookClientConfig) {

	//
	validationWcConfig.DeepCopyInto(&wcConfig)
	mutationWcConfig.DeepCopyInto(&wcConfig)

	if !reflect.ValueOf(validationWcConfig.Service).IsZero() {
		*validationWcConfig.Service.Path = *validationWcConfig.Service.Path + validationPath
		*mutationWcConfig.Service.Path = *mutationWcConfig.Service.Path + mutationPath
	}

	if !reflect.ValueOf(validationWcConfig.URL).IsZero() {
		*validationWcConfig.URL = *validationWcConfig.URL + validationPath
		*mutationWcConfig.URL = *mutationWcConfig.URL + mutationPath
	}

	return validationWcConfig, mutationWcConfig
}
