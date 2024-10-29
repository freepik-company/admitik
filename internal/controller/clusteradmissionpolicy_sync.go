package controller

import (
	"context"
	"fmt"
	coreLog "log"

	"freepik.com/admitik/api/v1alpha1"

	//
	admissionV1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ClusterAdmissionPolicyReconciler) SyncAdmissionPool(ctx context.Context, eventType watch.EventType, object *v1alpha1.ClusterAdmissionPolicy) (err error) {

	coreLog.Print("DOING ADMISSON POLICY STUFF")

	logger := log.FromContext(ctx)
	_ = logger

	// Obtain potential existing ValidatingWebhookConfiguration for this AdmissionPolicy
	metaWebhookObj := admissionV1.ValidatingWebhookConfiguration{}

	err = r.Get(ctx, types.NamespacedName{
		Name: object.Name,
	}, &metaWebhookObj)
	if err != nil {
		logger.Info(fmt.Sprintf("Error getting the ValidatingWebhookConfiguration: %s", err.Error()))
	}

	// Create a bare new 'webhooks' section for the ValidatingWebhookConfiguration and fill it
	tmpWebhookObj := admissionV1.ValidatingWebhook{}
	metaWebhookObj.Name = object.Name

	//
	ruleScope := admissionV1.ScopeType("*")
	ruleObj := admissionV1.RuleWithOperations{
		Rule: admissionV1.Rule{
			APIGroups:   []string{object.Spec.WatchedResources.Group},
			APIVersions: []string{object.Spec.WatchedResources.Version},
			Resources:   []string{object.Spec.WatchedResources.Resource},
			Scope:       &ruleScope,
		},
		Operations: object.Spec.WatchedResources.Operations,
	}

	tmpWebhookObj.Name = "validate.admitik.svc"
	tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	tmpWebhookObj.ClientConfig = r.Options.WebhookClientConfig
	tmpWebhookObj.Rules = append(tmpWebhookObj.Rules, ruleObj)
	tmpWebhookObj.MatchConditions = object.Spec.WatchedResources.MatchConditions

	sideEffectsClass := admissionV1.SideEffectClass(admissionV1.SideEffectClassNone)
	tmpWebhookObj.SideEffects = &sideEffectsClass

	// Replace the webhooks section in the ValidatingWebhookConfiguration
	metaWebhookObj.Webhooks = []admissionV1.ValidatingWebhook{tmpWebhookObj}

	// Sync changes to Kubernetes
	if errors.IsNotFound(err) {
		err = r.Create(ctx, &metaWebhookObj)
		if err != nil {
			logger.Info(fmt.Sprintf("Error creating ValidatingWebhookConfiguration: %s", err.Error()))
		}
	} else {
		err = r.Update(ctx, &metaWebhookObj)
		if err != nil {
			logger.Info(fmt.Sprintf("Error updating ValidatingWebhookConfiguration: %s", err.Error()))
		}
	}

	// 2. Update the AdmissionPool in concordance with the AdmissionPolicy
	// Splitting the rules by operations using the pattern: {group}/{version}/{resource}/{operation}

	//
	if eventType == watch.Added || eventType == watch.Modified {
		// 3.3 Guardar los AdmissionPolicies en los key que le toque
	} else if eventType == watch.Deleted {
		// Si el evento es eliminar, sacarlo de los keys que le toque
	}

	return nil
}
