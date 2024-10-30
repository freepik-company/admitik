package controller

import (
	"context"
	"fmt"
	coreLog "log"
	"slices"

	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/globals"

	//
	admissionV1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	AdmissionOperations = []admissionV1.OperationType{admissionV1.Create, admissionV1.Update, admissionV1.Delete, admissionV1.Connect}
)

func (r *ClusterAdmissionPolicyReconciler) SyncAdmissionPool(ctx context.Context, eventType watch.EventType, object *v1alpha1.ClusterAdmissionPolicy) (err error) {

	coreLog.Print("DOING CLUSTER ADMISSON POLICY STUFF")

	logger := log.FromContext(ctx)
	_ = logger

	// Replace wildcards in operations
	if slices.Contains(object.Spec.WatchedResources.Operations, admissionV1.OperationAll) {
		object.Spec.WatchedResources.Operations = AdmissionOperations
	}

	// Calculate the pool key pattern for operations
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

	coreLog.Printf("potential keys: %v", potentialAdmissionPoolKeyPatterns)
	coreLog.Printf("admited keys: %v", admissionPoolKeyPatterns)

	//
	switch eventType {

	case watch.Deleted:

		// Loop over those pool patterns containing operations present in being-deleted manifest to perform actions.
		for _, keyPattern := range admissionPoolKeyPatterns {

			newCapList := []v1alpha1.ClusterAdmissionPolicy{}

			// Review pool patterns NOT matching current manifest to clean potential
			// previous added objects that are not needed anymore
			for _, capObject := range globals.Application.ClusterAdmissionPolicyPool.Pool[keyPattern] {
				if capObject.Name == object.Name {
					continue
				}
				newCapList = append(newCapList, capObject)
			}

			globals.Application.ClusterAdmissionPolicyPool.Mutex.Lock()
			globals.Application.ClusterAdmissionPolicyPool.Pool[keyPattern] = newCapList
			globals.Application.ClusterAdmissionPolicyPool.Mutex.Unlock()
		}

	case watch.Modified:

		// Loop over all possible operations for the same pattern to perform actions.
		// Patterns are crafted as: /{group}/{version}/{resource}/{operation}
		for _, potentialKeyPattern := range potentialAdmissionPoolKeyPatterns {

			// Take actions for those patterns including operations actually desired in the user's manifest
			// When manifest includes: [CREATE, UPDATE], following code will loop
			// only over objects under: /.../CREATE and /.../UPDATE
			if slices.Contains(admissionPoolKeyPatterns, potentialKeyPattern) {

				for capIndex, capObject := range globals.Application.ClusterAdmissionPolicyPool.Pool[potentialKeyPattern] {

					// Object is present for this pattern, update it
					if capObject.Name == object.Name {
						globals.Application.ClusterAdmissionPolicyPool.Mutex.Lock()
						globals.Application.ClusterAdmissionPolicyPool.Pool[potentialKeyPattern][capIndex] = capObject
						globals.Application.ClusterAdmissionPolicyPool.Mutex.Unlock()
						break
					}
				}

				// Object is missing for this pattern, add it
				globals.Application.ClusterAdmissionPolicyPool.Pool[potentialKeyPattern] =
					append(globals.Application.ClusterAdmissionPolicyPool.Pool[potentialKeyPattern], *object)

				continue
			}

			// Review pool patterns NOT matching current manifest to clean potential
			// previous added objects that are not needed anymore
			newCapList := []v1alpha1.ClusterAdmissionPolicy{}
			for _, capObject := range globals.Application.ClusterAdmissionPolicyPool.Pool[potentialKeyPattern] {
				if capObject.Name == object.Name {
					continue
				}
				newCapList = append(newCapList, capObject)
			}

			if len(newCapList) == 0 {
				continue
			}

			globals.Application.ClusterAdmissionPolicyPool.Mutex.Lock()
			globals.Application.ClusterAdmissionPolicyPool.Pool[potentialKeyPattern] = newCapList
			globals.Application.ClusterAdmissionPolicyPool.Mutex.Unlock()
		}
	}

	//coreLog.Print(globals.Application.ClusterAdmissionPolicyPool.Pool)

	for key, item := range globals.Application.ClusterAdmissionPolicyPool.Pool {

		coreLog.Printf("Las key '%s', num cosos dentro: '%d'", key, len(item))

	}

	// 1. Check if an existing ClusterAdmissionPolicy exists in the AdmissionPool calling the same watchedResources

	// // Obtain potential existing ValidatingWebhookConfiguration for this AdmissionPolicy
	// metaWebhookObj := admissionV1.ValidatingWebhookConfiguration{}

	// err = r.Get(ctx, types.NamespacedName{
	// 	Name: object.Name,
	// }, &metaWebhookObj)
	// if err != nil {
	// 	logger.Info(fmt.Sprintf("Error getting the ValidatingWebhookConfiguration: %s", err.Error()))
	// }

	// // Create a bare new 'webhooks' section for the ValidatingWebhookConfiguration and fill it
	// tmpWebhookObj := admissionV1.ValidatingWebhook{}
	// metaWebhookObj.Name = object.Name

	// //
	// ruleScope := admissionV1.ScopeType("*")
	// ruleObj := admissionV1.RuleWithOperations{
	// 	Rule: admissionV1.Rule{
	// 		APIGroups:   []string{object.Spec.WatchedResources.Group},
	// 		APIVersions: []string{object.Spec.WatchedResources.Version},
	// 		Resources:   []string{object.Spec.WatchedResources.Resource},
	// 		Scope:       &ruleScope,
	// 	},
	// 	Operations: object.Spec.WatchedResources.Operations,
	// }

	// tmpWebhookObj.Name = "validate.admitik.svc"
	// tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	// tmpWebhookObj.ClientConfig = r.Options.WebhookClientConfig
	// tmpWebhookObj.Rules = append(tmpWebhookObj.Rules, ruleObj)
	// tmpWebhookObj.MatchConditions = object.Spec.WatchedResources.MatchConditions

	// sideEffectsClass := admissionV1.SideEffectClass(admissionV1.SideEffectClassNone)
	// tmpWebhookObj.SideEffects = &sideEffectsClass

	// // Replace the webhooks section in the ValidatingWebhookConfiguration
	// metaWebhookObj.Webhooks = []admissionV1.ValidatingWebhook{tmpWebhookObj}

	// // Sync changes to Kubernetes
	// if errors.IsNotFound(err) {
	// 	err = r.Create(ctx, &metaWebhookObj)
	// 	if err != nil {
	// 		logger.Info(fmt.Sprintf("Error creating ValidatingWebhookConfiguration: %s", err.Error()))
	// 	}
	// } else {
	// 	err = r.Update(ctx, &metaWebhookObj)
	// 	if err != nil {
	// 		logger.Info(fmt.Sprintf("Error updating ValidatingWebhookConfiguration: %s", err.Error()))
	// 	}
	// }

	// // 2. Update the AdmissionPool in concordance with the AdmissionPolicy
	// // Splitting the rules by operations using the pattern: {group}/{version}/{resource}/{operation}

	// //
	// if eventType == watch.Added || eventType == watch.Modified {
	// 	// 3.3 Guardar los AdmissionPolicies en los key que le toque
	// } else if eventType == watch.Deleted {
	// 	// Si el evento es eliminar, sacarlo de los keys que le toque
	// }

	return nil
}
