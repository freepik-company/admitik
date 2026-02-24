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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/freepik-company/admitik/internal/controller"
	"github.com/freepik-company/admitik/internal/globals"
)

// CleanupGeneratedResources finds and deletes all resources labeled as generated
// by the given policy name and kind. It discovers all API resources and scans
// for matching labels across all namespaces.
func CleanupGeneratedResources(ctx context.Context, policyName, policyKind string) error {
	labelSelector := fmt.Sprintf("%s=%s,%s=%s",
		controller.GeneratedByPolicyLabel, policyName,
		controller.GeneratedByPolicyKind, policyKind)

	_, apiGroupResourcesLists, err := globals.Application.KubeDiscoveryClient.ServerGroupsAndResources()
	if err != nil {
		return fmt.Errorf("failed discovering API resources: %w", err)
	}

	var lastErr error
	for _, apiGroupResourcesList := range apiGroupResourcesLists {
		gv, parseErr := schema.ParseGroupVersion(apiGroupResourcesList.GroupVersion)
		if parseErr != nil {
			continue
		}

		for _, apiResource := range apiGroupResourcesList.APIResources {
			if !containsVerb(apiResource.Verbs, "list") || !containsVerb(apiResource.Verbs, "delete") {
				continue
			}

			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: apiResource.Name,
			}

			var resourceClient dynamic.ResourceInterface
			if apiResource.Namespaced {
				resourceClient = globals.Application.KubeRawClient.Resource(gvr).Namespace("")
			} else {
				resourceClient = globals.Application.KubeRawClient.Resource(gvr)
			}

			list, listErr := resourceClient.List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if listErr != nil {
				continue
			}

			for _, item := range list.Items {
				var delClient dynamic.ResourceInterface
				if item.GetNamespace() != "" {
					delClient = globals.Application.KubeRawClient.Resource(gvr).Namespace(item.GetNamespace())
				} else {
					delClient = globals.Application.KubeRawClient.Resource(gvr)
				}

				deleteErr := delClient.Delete(ctx, item.GetName(), metav1.DeleteOptions{})
				if deleteErr != nil {
					lastErr = deleteErr
				}
			}
		}
	}

	return lastErr
}

func containsVerb(verbs metav1.Verbs, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}
