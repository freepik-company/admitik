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

package clusteradmissionpolicy

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
	// ClusterAdmissionPolicyRequesterName represents the name of the requester that this controller will set
	// when asking SourcesController to SyncWatchers()
	ClusterAdmissionPolicyRequesterName = "ClusterAdmissionPolicyController"

	// ValidatingWebhookConfigurationName represents the name of the ValidatingWebhookConfiguration resource
	// that will be created or updated in Kubernetes to forward requests to the admissions webserver
	ValidatingWebhookConfigurationName = "admitik-cluster-admission-policy"
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

func (r *ClusterAdmissionPolicyController) SyncAdmissionPool(ctx context.Context, eventType watch.EventType, object *v1alpha1.ClusterAdmissionPolicy) (err error) {

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
		policyPoolLocations := r.policyPool.getClusterAdmissionPoolPolicyIndexes(object)

		for resourcePattern, policyIndex := range policyPoolLocations {
			r.policyPool.deleteClusterAdmissionPoolPolicyByIndex(resourcePattern, policyIndex)
		}

	case watch.Modified:

		// Loop over every potential operation related to the same GVR.
		// Patterns are crafted as: /{group}/{version}/{resource}/{operation}
		for _, potentialKeyPattern := range potentialAdmissionPoolKeyPatterns {

			// Take actions for those patterns including operations actually desired in the user's manifest
			// When manifest includes: [CREATE, UPDATE], following code will loop
			// only over objects under: /.../CREATE and /.../UPDATE
			if slices.Contains(admissionPoolKeyPatterns, potentialKeyPattern) {

				objectIndex := r.policyPool.getClusterAdmissionPolicyIndex(potentialKeyPattern, object)

				// Object is present for this pattern, update it
				// TODO: Craft an update function that truly updates this
				if objectIndex >= 0 {
					r.policyPool.deleteClusterAdmissionPoolPolicyByIndex(potentialKeyPattern, objectIndex)
					r.policyPool.createClusterAdmissionPoolPolicy(potentialKeyPattern, object)
					continue
				}

				// Object is missing for this pattern, add it
				r.policyPool.createClusterAdmissionPoolPolicy(potentialKeyPattern, object)
			}
		}

		// Review pool patterns NOT matching current object to clean potential
		// previous added objects that are not needed anymore
		policyPoolLocations := r.policyPool.getClusterAdmissionPoolPolicyIndexes(object)

		for resourcePattern, policyIndex := range policyPoolLocations {

			if !slices.Contains(admissionPoolKeyPatterns, resourcePattern) {
				r.policyPool.deleteClusterAdmissionPoolPolicyByIndex(resourcePattern, policyIndex)
			}
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

	// Ask SourcesController to watch all the sources
	watchersList := r.getAllSourcesReferences()

	err = r.dependencies.Sources.SyncWatchers(watchersList, ClusterAdmissionPolicyRequesterName)
	if err != nil {
		err = fmt.Errorf("error syncing watchers: %s", err.Error())
		return
	}

	return nil
}

// getValidatingWebhookConfiguration return a ValidatingWebhookConfiguration object that is built based on
// previous existing one in Kubernetes and the current pool keys extracted from ClusterAdmissionPolicy.spec.watchedResources
func (r *ClusterAdmissionPolicyController) getMergedValidatingWebhookConfiguration(ctx context.Context) (
	vwConfig admissionregv1.ValidatingWebhookConfiguration, alreadyCreated bool, err error) {

	// Craft ValidatingWebhookConfiguration rules based on the pool keys
	currentVwcRules := []admissionregv1.RuleWithOperations{}
	for resourcePattern, _ := range r.policyPool.Pool {

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

	if !strings.EqualFold(string(metaWebhookObj.UID), "") {
		alreadyCreated = true
	}

	// Create a bare new 'webhooks' section for the ValidatingWebhookConfiguration and fill it
	tmpWebhookObj := admissionregv1.ValidatingWebhook{}
	timeoutSecondsConverted := int32(r.options.WebhookTimeout)

	tmpWebhookObj.Name = "validate.admitik.svc"
	tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	tmpWebhookObj.ClientConfig = r.options.WebhookClientConfig
	tmpWebhookObj.Rules = currentVwcRules
	tmpWebhookObj.TimeoutSeconds = &timeoutSecondsConverted

	sideEffectsClass := admissionregv1.SideEffectClass(admissionregv1.SideEffectClassNone)
	tmpWebhookObj.SideEffects = &sideEffectsClass

	// Replace the webhooks section in the ValidatingWebhookConfiguration
	metaWebhookObj.Webhooks = []admissionregv1.ValidatingWebhook{tmpWebhookObj}

	return metaWebhookObj, alreadyCreated, nil
}

// getAllSourcesReferences iterates over all the ClusterAdmissionPolicy objects and
// create a list with all the sources in the format GVRNN (Group/Version/Resource/Namespace/Name)
func (r *ClusterAdmissionPolicyController) getAllSourcesReferences() (references []string) {

	r.policyPool.Mutex.Lock()
	defer r.policyPool.Mutex.Unlock()

	//
	for _, capList := range r.policyPool.Pool {
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
