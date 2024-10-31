package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"

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
	AdmissionOperations = []admissionregv1.OperationType{admissionregv1.Create, admissionregv1.Update, admissionregv1.Delete, admissionregv1.Connect}

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

	tmpWebhookObj.Name = "validate.admitik.svc"
	tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	tmpWebhookObj.ClientConfig = r.Options.WebhookClientConfig
	tmpWebhookObj.Rules = currentVwcRules
	//tmpWebhookObj.MatchConditions = object.Spec.WatchedResources.MatchConditions

	sideEffectsClass := admissionregv1.SideEffectClass(admissionregv1.SideEffectClassNone)
	tmpWebhookObj.SideEffects = &sideEffectsClass

	// Replace the webhooks section in the ValidatingWebhookConfiguration
	metaWebhookObj.Webhooks = []admissionregv1.ValidatingWebhook{tmpWebhookObj}

	// Sync changes to Kubernetes
	if errors.IsNotFound(err) {
		err = r.Create(ctx, &metaWebhookObj)
		if err != nil {
			err = fmt.Errorf("error creating ValidatingWebhookConfiguration '%s': %s",
				ValidatingWebhookConfigurationName, err.Error())
			return
		}
	} else {
		err = r.Update(ctx, &metaWebhookObj)
		if err != nil {
			err = fmt.Errorf("error updating ValidatingWebhookConfiguration '%s': %s",
				ValidatingWebhookConfigurationName, err.Error())
			return
		}
	}

	return nil
}
