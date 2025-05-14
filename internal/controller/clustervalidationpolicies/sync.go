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

package clustervalidationpolicies

import (
	"context"
	"fmt"
	"slices"
	"strings"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"freepik.com/admitik/api/v1alpha1"
)

const (

	//
	resourceUpdatedMessage  = "A ClusterValidationPolicy was modified: will be updated into the internal registry"
	resourceDeletionMessage = "A ClusterValidationPolicy was deleted: will be deleted from internal registry"
)

const (
	// ValidatingWebhookConfigurationName represents the name of the ValidatingWebhookConfiguration resource
	// that will be created or updated in Kubernetes to forward validation requests to the admissions webserver
	ValidatingWebhookConfigurationName = "admitik-cluster-validation-policy"
)

var (
	AdmissionOperations = []admissionregv1.OperationType{
		admissionregv1.Create,
		admissionregv1.Update,
		admissionregv1.Delete,
		admissionregv1.Connect}

	//
	ValidatingWebhookConfigurationRuleScopeAll = admissionregv1.ScopeType("*")
)

// ReconcileClusterValidationPolicy keeps internal ClusterValidationPolicy resources' registry up-to-date
func (r *ClusterValidationPolicyReconciler) ReconcileClusterValidationPolicy(ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterValidationPolicy) (err error) {
	logger := log.FromContext(ctx)

	// Replace wildcards in operations
	if slices.Contains(resourceManifest.Spec.InterceptedResources.Operations, admissionregv1.OperationAll) {
		resourceManifest.Spec.InterceptedResources.Operations = AdmissionOperations
	}

	// Update the registry
	var desiredWatchedTypes []string
	for _, operation := range resourceManifest.Spec.InterceptedResources.Operations {

		// Skip unsupported operations
		if !slices.Contains(AdmissionOperations, operation) {
			continue
		}

		// Create the key-pattern and store it for later cleaning
		// Duplicated operations will be skipped
		// Duplicated operations will be skipped
		watchedType := strings.Join([]string{
			resourceManifest.Spec.InterceptedResources.Group,
			resourceManifest.Spec.InterceptedResources.Version,
			resourceManifest.Spec.InterceptedResources.Resource,
			string(operation),
		}, "/")

		if slices.Contains(desiredWatchedTypes, watchedType) {
			continue
		}
		desiredWatchedTypes = append(desiredWatchedTypes, watchedType)

		// Handle deletion requests
		if eventType == watch.Deleted {
			logger.Info(resourceDeletionMessage, "watcher", watchedType)

			r.Dependencies.ClusterValidationPoliciesRegistry.RemoveResource(watchedType, resourceManifest)
		}

		// Handle creation/update requests
		if eventType == watch.Modified {
			logger.Info(resourceUpdatedMessage, "watcher", watchedType)

			r.Dependencies.ClusterValidationPoliciesRegistry.RemoveResource(watchedType, resourceManifest)
			r.Dependencies.ClusterValidationPoliciesRegistry.AddResource(watchedType, resourceManifest)
		}
	}

	// Clean non-desired watched types. This is needed for updates where the user
	// reduces the amount of watched operations on watched resources
	for _, registeredResourceType := range r.Dependencies.ClusterValidationPoliciesRegistry.GetRegisteredResourceTypes() {
		if !slices.Contains(desiredWatchedTypes, registeredResourceType) {
			r.Dependencies.ClusterValidationPoliciesRegistry.RemoveResource(registeredResourceType, resourceManifest)
		}
	}

	// Craft ValidatingWebhookConfiguration rules based on the previous existing one and current pool keys
	metaWebhookObj, alreadyCreated, err := r.getMergedValidatingWebhookConfiguration(ctx)
	if err != nil {
		err = fmt.Errorf("error building ValidatingWebhookConfiguration '%s': %s",
			ValidatingWebhookConfigurationName, err.Error())
		return
	}

	// Sync changes to Kubernetes
	if !alreadyCreated {
		err = r.Create(ctx, &metaWebhookObj)
		if err != nil {
			err = fmt.Errorf("error creating ValidatingWebhookConfiguration in Kubernetes'%s': %s",
				ValidatingWebhookConfigurationName, err.Error())
			return
		}
	} else {
		err = r.Update(ctx, &metaWebhookObj)
		if err != nil {
			err = fmt.Errorf("error updating ValidatingWebhookConfiguration in Kubernetes '%s': %s",
				ValidatingWebhookConfigurationName, err.Error())
			return
		}
	}

	return nil
}

// getValidatingWebhookConfiguration return a ValidatingWebhookConfiguration object that is built based on
// previous existing one in Kubernetes and the current pool keys extracted from ClusterValidationPolicy.spec.InterceptedResources
func (r *ClusterValidationPolicyReconciler) getMergedValidatingWebhookConfiguration(ctx context.Context) (
	vwConfig admissionregv1.ValidatingWebhookConfiguration, alreadyCreated bool, err error) {

	// Craft ValidatingWebhookConfiguration rules based on the pool keys
	currentVwcRules := []admissionregv1.RuleWithOperations{}
	InterceptedResourcesPatterns := r.Dependencies.ClusterValidationPoliciesRegistry.GetRegisteredResourceTypes()

	for _, resourcePattern := range InterceptedResourcesPatterns {

		resourcePatternParts := strings.Split(resourcePattern, "/")
		if len(resourcePatternParts) != 4 {
			err = fmt.Errorf("some key-pattern is invalid on ClusterValidationPolicy registry. Open an issue to fix it")
			return
		}

		tmpRule := admissionregv1.RuleWithOperations{
			Rule: admissionregv1.Rule{
				APIGroups:   []string{resourcePatternParts[0]},
				APIVersions: []string{resourcePatternParts[1]},
				Resources:   []string{resourcePatternParts[2]},
				Scope:       &ValidatingWebhookConfigurationRuleScopeAll,
			},
			Operations: []admissionregv1.OperationType{admissionregv1.OperationType(resourcePatternParts[3])},
		}
		currentVwcRules = append(currentVwcRules, tmpRule)
	}

	// Obtain potential existing ValidatingWebhookConfiguration
	metaWebhookObj := admissionregv1.ValidatingWebhookConfiguration{}
	metaWebhookObj.Name = ValidatingWebhookConfigurationName

	err = r.Get(ctx, types.NamespacedName{
		Name: ValidatingWebhookConfigurationName,
	}, &metaWebhookObj)
	if err != nil {
		if !errors.IsNotFound(err) {
			err = fmt.Errorf("error getting the ValidatingWebhookConfiguration '%s' : %s",
				ValidatingWebhookConfigurationName, err.Error())
			return
		}
	}

	if !strings.EqualFold(string(metaWebhookObj.UID), "") {
		alreadyCreated = true
	}

	// Create a bare new 'webhooks' section for the ValidatingWebhookConfiguration and fill it
	tmpWebhookObj := admissionregv1.ValidatingWebhook{}
	timeoutSecondsConverted := int32(r.Options.WebhookTimeout)

	tmpWebhookObj.Name = "validate.admitik.svc"
	tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	tmpWebhookObj.ClientConfig = r.Options.WebhookClientConfig
	tmpWebhookObj.Rules = currentVwcRules
	tmpWebhookObj.TimeoutSeconds = &timeoutSecondsConverted

	sideEffectsClass := admissionregv1.SideEffectClass(admissionregv1.SideEffectClassNone)
	tmpWebhookObj.SideEffects = &sideEffectsClass

	// Replace the webhooks section in the ValidatingWebhookConfiguration
	metaWebhookObj.Webhooks = []admissionregv1.ValidatingWebhook{tmpWebhookObj}

	return metaWebhookObj, alreadyCreated, nil
}
