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

// TODO: Decouple this controller from 'globals' package

package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/globals"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ValidatingWebhookConfigurationName = "admitik-cluster-admission-policy"
)

var (
	AdmissionOperations = []admissionregv1.OperationType{
		admissionregv1.Create, admissionregv1.Update, admissionregv1.Delete, admissionregv1.Connect}

	//
	ValidatingWebhookConfigurationRuleScopeAll = admissionregv1.ScopeType("*")
)

func (r *ClusterAdmissionPolicyReconciler) SyncAdmissionPool(ctx context.Context, eventType watch.EventType, object *v1alpha1.ClusterAdmissionPolicy) (err error) {

	logger := log.FromContext(ctx)
	_ = logger

	// Replace wildcards in operations
	if slices.Contains(object.Spec.WatchedResources.Operations, admissionregv1.OperationAll) {
		object.Spec.WatchedResources.Operations = AdmissionOperations
	}

	// Calculate the pool key-pattern for operations
	// /{group}/{version}/{resource}/{operation}
	var potentialAdmissionPoolKeyPatterns []string
	var admissionPoolKeyPatterns []string
	for _, operation := range AdmissionOperations {

		keyPattern := fmt.Sprintf("/%s/%s/%s/%s",
			object.Spec.WatchedResources.Group,
			object.Spec.WatchedResources.Version,
			object.Spec.WatchedResources.Resource,
			operation)

		potentialAdmissionPoolKeyPatterns = append(potentialAdmissionPoolKeyPatterns, keyPattern)

		if slices.Contains(object.Spec.WatchedResources.Operations, operation) {
			admissionPoolKeyPatterns = append(admissionPoolKeyPatterns, keyPattern)
		}
	}

	//
	switch eventType {

	case watch.Deleted:

		// Review pool key-patterns matching current object to
		// clean objects that are not needed anymore
		policyPoolLocations := globals.GetClusterAdmissionPoolPolicyIndexes(object)

		for resourcePattern, policyIndex := range policyPoolLocations {
			globals.DeleteClusterAdmissionPoolPolicyByIndex(resourcePattern, policyIndex)
		}

	case watch.Modified:

		// Loop over every potential operation related to the same GVR.
		// Patterns are crafted as: /{group}/{version}/{resource}/{operation}
		for _, potentialKeyPattern := range potentialAdmissionPoolKeyPatterns {

			// Take actions for those patterns including operations actually desired in the user's manifest
			// When manifest includes: [CREATE, UPDATE], following code will loop
			// only over objects under: /.../CREATE and /.../UPDATE
			if slices.Contains(admissionPoolKeyPatterns, potentialKeyPattern) {

				objectIndex := globals.GetClusterAdmissionPolicyIndex(potentialKeyPattern, object)

				// Object is present for this pattern, update it
				// TODO: Craft an update function that truly updates this
				if objectIndex >= 0 {
					globals.DeleteClusterAdmissionPoolPolicyByIndex(potentialKeyPattern, objectIndex)
					globals.CreateClusterAdmissionPoolPolicy(potentialKeyPattern, object)
					continue
				}

				// Object is missing for this pattern, add it
				globals.CreateClusterAdmissionPoolPolicy(potentialKeyPattern, object)
			}
		}

		// Review pool patterns NOT matching current object to clean potential
		// previous added objects that are not needed anymore
		policyPoolLocations := globals.GetClusterAdmissionPoolPolicyIndexes(object)

		for resourcePattern, policyIndex := range policyPoolLocations {

			if !slices.Contains(admissionPoolKeyPatterns, resourcePattern) {
				globals.DeleteClusterAdmissionPoolPolicyByIndex(resourcePattern, policyIndex)
			}
		}
	}

	// Craft ValidatingWebhookConfiguration rules based on the previous existing one and current pool keys
	metaWebhookObj, err := r.getMergedValidatingWebhookConfiguration(ctx)
	if err != nil {
		err = fmt.Errorf("error building ValidatingWebhookConfiguration '%s': %s",
			ValidatingWebhookConfigurationName, err.Error())
		return
	}

	// Sync changes to Kubernetes
	if errors.IsNotFound(err) {
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

	// Ask SourcesController to watch all the sources
	watchersList := r.getAllSourcesReferences()

	err = globals.Application.SourceController.SyncWatchers(watchersList)
	if err != nil {
		err = fmt.Errorf("error syncing watchers: %s", err.Error())
		return
	}

	return nil
}

// getValidatingWebhookConfiguration return a ValidatingWebhookConfiguration object that is built based on
// previous existing one in Kubernetes and the current pool keys extracted from ClusterAdmissionPolicy.spec.watchedResources
func (r *ClusterAdmissionPolicyReconciler) getMergedValidatingWebhookConfiguration(ctx context.Context) (
	vwConfig admissionregv1.ValidatingWebhookConfiguration, err error) {

	// Craft ValidatingWebhookConfiguration rules based on the pool keys
	currentVwcRules := []admissionregv1.RuleWithOperations{}
	for resourcePattern, _ := range globals.Application.ClusterAdmissionPolicyPool.Pool {

		resourcePatternParts := strings.Split(resourcePattern, "/")
		if len(resourcePatternParts) != 5 {
			err = fmt.Errorf("some key-pattern is invalid on ClusterAdmissionPolicyPool. Open an issue to fix it")
			return
		}

		tmpRule := admissionregv1.RuleWithOperations{
			Rule: admissionregv1.Rule{
				APIGroups:   []string{resourcePatternParts[1]},
				APIVersions: []string{resourcePatternParts[2]},
				Resources:   []string{resourcePatternParts[3]},
				Scope:       &ValidatingWebhookConfigurationRuleScopeAll,
			},
			Operations: []admissionregv1.OperationType{admissionregv1.OperationType(resourcePatternParts[4])},
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

	return metaWebhookObj, nil
}

// getAllSourcesReferences iterates over all the ClusterAdmissionPolicy objects and
// create a list with all the sources in the format GVRNN (Group/Version/Resource/Namespace/Name)
func (r *ClusterAdmissionPolicyReconciler) getAllSourcesReferences() (references []string) {

	globals.Application.ClusterAdmissionPolicyPool.Mutex.Lock()
	defer globals.Application.ClusterAdmissionPolicyPool.Mutex.Unlock()

	//
	for _, capList := range globals.Application.ClusterAdmissionPolicyPool.Pool {
		for _, capObject := range capList {
			for _, capObjSource := range capObject.Spec.Sources {

				sourceString := fmt.Sprintf("%s/%s/%s/%s/%s",
					capObjSource.Group,
					capObjSource.Version,
					capObjSource.Resource,
					capObjSource.Namespace,
					capObjSource.Namespace,
				)

				if slices.Contains(references, sourceString) {
					continue
				}

				references = append(references, sourceString)

			}
		}
	}

	return references
}
