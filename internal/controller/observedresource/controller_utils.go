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

package observedresource

import (
	"freepik.com/admitik/internal/globals"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

// GVKR represents TODO
type GVKR struct {
	GVK         schema.GroupVersionKind
	Resource    string
	Subresource string

	//
	Namespaced bool
}

// fetchKubeAvailableResources TODO
func fetchKubeAvailableResources() (resources *[]GVKR, err error) {

	resources = &[]GVKR{}

	_, apiGroupResourcesLists, err := globals.Application.KubeDiscoveryClient.ServerGroupsAndResources()
	if err != nil {
		return resources, err
	}

	for _, apiGroupResourcesList := range apiGroupResourcesLists {

		var tmpGroup, tmpVersion string
		groupVersionParts := strings.Split(apiGroupResourcesList.GroupVersion, "/")
		tmpVersion = groupVersionParts[0]
		if len(groupVersionParts) > 1 {
			tmpGroup = groupVersionParts[0]
			tmpVersion = groupVersionParts[1]
		}

		for _, apiGroupResource := range apiGroupResourcesList.APIResources {

			tmpObj := &GVKR{}

			tmpObj.Resource = apiGroupResource.Name
			resourceParts := strings.Split(tmpObj.Resource, "/")
			if len(resourceParts) > 1 {
				tmpObj.Resource = resourceParts[0]
				tmpObj.Subresource = strings.Join(resourceParts[1:], "/")
			}

			tmpObj.GVK.Group = tmpGroup
			tmpObj.GVK.Version = tmpVersion
			tmpObj.GVK.Kind = apiGroupResource.Kind

			tmpObj.Namespaced = apiGroupResource.Namespaced

			*resources = append(*resources, *tmpObj)
		}
	}

	return resources, nil
}

// getResourceFromGvk TODO
func getResourceFromGvk(resourceList *[]GVKR, gvk schema.GroupVersionKind) string {

	for _, object := range *resourceList {
		if object.GVK == gvk {
			return object.Resource
		}
	}
	return ""
}
