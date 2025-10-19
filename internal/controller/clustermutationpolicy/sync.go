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

package clustermutationpolicy

import (
	"context"
	"fmt"
	"github.com/freepik-company/admitik/internal/pubsub"
	"slices"
	"strings"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/controller"
)

const (

	//
	resourceUpdatedMessage  = "A ClusterMutationPolicy was modified: will be updated into the internal registry"
	resourceDeletionMessage = "A ClusterMutationPolicy was deleted: will be deleted from internal registry"
)

const (
	// MutatingWebhookConfigurationName represents the name of the MutatingWebhookConfiguration resource
	// that will be created or updated in Kubernetes to forward requests to the admissions webserver
	MutatingWebhookConfigurationName = "admitik-cluster-mutation-policy"
)

var (
	MutationOperations = []admissionregv1.OperationType{
		admissionregv1.Create,
		admissionregv1.Update,
		admissionregv1.Delete,
		admissionregv1.Connect}

	//
	MutatingWebhookConfigurationRuleScopeAll = admissionregv1.ScopeType("*")
)

// ReconcileClusterMutationPolicy keeps internal ClusterMutationPolicy resources' registry up-to-date
func (r *ClusterMutationPolicyReconciler) ReconcileClusterMutationPolicy(ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterMutationPolicy) (err error) {
	logger := log.FromContext(ctx)

	// Publish the event in the registry event bus
	r.Dependencies.ClusterMutationPolicyRegistry.Broadcaster.Publish(pubsub.Event[*v1alpha1.ClusterMutationPolicy]{
		Type:   string(eventType),
		Object: resourceManifest,
	})

	// Update the registry store
	var desiredWatchedTypes []string
	for _, intercResourceGroup := range resourceManifest.Spec.InterceptedResources {

		// Replace wildcards in operations
		if slices.Contains(intercResourceGroup.Operations, admissionregv1.OperationAll) {
			intercResourceGroup.Operations = MutationOperations
		}

		//
		for _, operation := range intercResourceGroup.Operations {

			// Skip unsupported operations
			if !slices.Contains(MutationOperations, operation) {
				continue
			}

			//
			watchedType := strings.Join([]string{
				intercResourceGroup.Group,
				intercResourceGroup.Version,
				intercResourceGroup.Resource,
				string(operation),
			}, "/")

			// Handle deletion requests
			if eventType == watch.Deleted {
				logger.Info(resourceDeletionMessage, "watcher", watchedType)
				r.Dependencies.ClusterMutationPolicyRegistry.Store.RemoveResource(watchedType, resourceManifest)
				continue
			}

			// Handle creation/update requests
			if eventType == watch.Modified {
				logger.Info(resourceUpdatedMessage, "watcher", watchedType)

				// Avoid adding those already added.
				// This prevents user from defining the group more than once per manifest
				if slices.Contains(desiredWatchedTypes, watchedType) {
					continue
				}
				desiredWatchedTypes = append(desiredWatchedTypes, watchedType)
				r.Dependencies.ClusterMutationPolicyRegistry.Store.AddOrUpdateResource(watchedType, resourceManifest)
			}

			// Re-sort collection by priority (ascending order)
			r.Dependencies.ClusterMutationPolicyRegistry.SortCollection(watchedType, func(a, b *v1alpha1.ClusterMutationPolicy) bool {
				return a.Spec.Priority < b.Spec.Priority
			})
		}
	}

	// Clean non-desired watched types. This is needed for updates where the user
	// reduces the amount of watched operations on watched resources
	for _, registeredResourceType := range r.Dependencies.ClusterMutationPolicyRegistry.Store.GetCollectionNames() {
		if !slices.Contains(desiredWatchedTypes, registeredResourceType) {
			r.Dependencies.ClusterMutationPolicyRegistry.Store.RemoveResource(registeredResourceType, resourceManifest)
		}
	}

	// Craft MutatingWebhookConfiguration rules based on the previous existing one and current pool keys
	metaWebhookObj, alreadyCreated, err := r.getMergedMutatingWebhookConfiguration(ctx)
	if err != nil {
		err = fmt.Errorf("error building MutatingWebhookConfiguration '%s': %s",
			MutatingWebhookConfigurationName, err.Error())
		return
	}

	// Sync changes to Kubernetes
	if !alreadyCreated {
		err = r.Create(ctx, &metaWebhookObj)
		if err != nil {
			err = fmt.Errorf("error creating MutatingWebhookConfiguration in Kubernetes'%s': %s",
				MutatingWebhookConfigurationName, err.Error())
			return
		}
	} else {
		err = r.Update(ctx, &metaWebhookObj)
		if err != nil {
			err = fmt.Errorf("error updating MutatingWebhookConfiguration in Kubernetes '%s': %s",
				MutatingWebhookConfigurationName, err.Error())
			return
		}
	}

	return nil
}

// getValidatingWebhookConfiguration return a ValidatingWebhookConfiguration object that is built based on
// previous existing one in Kubernetes and the current pool keys extracted from ClusterMutationPolicy.spec.InterceptedResources
func (r *ClusterMutationPolicyReconciler) getMergedMutatingWebhookConfiguration(ctx context.Context) (
	mwConfig admissionregv1.MutatingWebhookConfiguration, alreadyCreated bool, err error) {

	// Craft ValidatingWebhookConfiguration rules based on the pool keys
	currentVwcRules := []admissionregv1.RuleWithOperations{}
	InterceptedResourcesPatterns := r.Dependencies.ClusterMutationPolicyRegistry.Store.GetCollectionNames()

	for _, resourcePattern := range InterceptedResourcesPatterns {

		resourcePatternParts := strings.Split(resourcePattern, "/")
		if len(resourcePatternParts) != 4 {
			err = fmt.Errorf("some key-pattern is invalid on ClusterMutationPolicyPool. Open an issue to fix it")
			return
		}

		tmpRule := admissionregv1.RuleWithOperations{
			Rule: admissionregv1.Rule{
				APIGroups:   []string{resourcePatternParts[0]},
				APIVersions: []string{resourcePatternParts[1]},
				Resources:   []string{resourcePatternParts[2]},
				Scope:       &MutatingWebhookConfigurationRuleScopeAll,
			},
			Operations: []admissionregv1.OperationType{admissionregv1.OperationType(resourcePatternParts[3])},
		}
		currentVwcRules = append(currentVwcRules, tmpRule)
	}

	// Obtain potential existing ValidatingWebhookConfiguration
	metaWebhookObj := admissionregv1.MutatingWebhookConfiguration{}
	metaWebhookObj.Name = MutatingWebhookConfigurationName

	err = r.Get(ctx, types.NamespacedName{
		Name: MutatingWebhookConfigurationName,
	}, &metaWebhookObj)
	if err != nil {
		if !errors.IsNotFound(err) {
			err = fmt.Errorf("error getting the MutatingWebhookConfiguration '%s' : %s",
				MutatingWebhookConfigurationName, err.Error())
			return
		}
	}

	if !strings.EqualFold(string(metaWebhookObj.UID), "") {
		alreadyCreated = true
	}

	// Create a bare new 'webhooks' section for the ValidatingWebhookConfiguration and fill it
	tmpWebhookObj := admissionregv1.MutatingWebhook{}
	timeoutSecondsConverted := int32(r.Options.WebhookTimeout)

	tmpWebhookObj.Name = "mutate.admitik.svc"
	tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	tmpWebhookObj.ClientConfig = r.Options.WebhookClientConfig
	tmpWebhookObj.Rules = currentVwcRules
	tmpWebhookObj.TimeoutSeconds = &timeoutSecondsConverted

	// Ignore sensitive namespaces to avoid breaking Kubernetes essential services or chicken-egg scenarios
	selectedNamespaces := []string{}
	if !strings.EqualFold(r.Options.ExcludedAdmissionNamespaces, "") {
		selectedNamespaces = strings.Split(r.Options.ExcludedAdmissionNamespaces, ",")
	}
	if r.Options.ExcludeAdmissionSelfNamespace {
		selectedNamespaces = append(selectedNamespaces, r.Options.CurrentNamespace)
	}

	if len(selectedNamespaces) > 0 {
		tmpWebhookObj.NamespaceSelector = &v1.LabelSelector{}
		tmpWebhookObj.NamespaceSelector.MatchExpressions = []v1.LabelSelectorRequirement{{
			Key:      "kubernetes.io/metadata.name",
			Operator: v1.LabelSelectorOpNotIn,
			Values:   selectedNamespaces,
		}}
	}

	// Ignore admission for resources meeting a special label
	if r.Options.EnableSpecialLabels {
		tmpWebhookObj.ObjectSelector = &v1.LabelSelector{}
		tmpWebhookObj.ObjectSelector.MatchExpressions = []v1.LabelSelectorRequirement{{
			Key:      controller.IgnoreAdmissionLabel,
			Operator: v1.LabelSelectorOpDoesNotExist,
		}}
	}

	sideEffectsClass := admissionregv1.SideEffectClass(admissionregv1.SideEffectClassNone)
	tmpWebhookObj.SideEffects = &sideEffectsClass

	// Replace the webhooks section in the ValidatingWebhookConfiguration
	metaWebhookObj.Webhooks = []admissionregv1.MutatingWebhook{tmpWebhookObj}

	return metaWebhookObj, alreadyCreated, nil
}
