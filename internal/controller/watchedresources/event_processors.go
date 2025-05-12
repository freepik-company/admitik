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

package watchedresources

import (
	corelog "log"

	"k8s.io/apimachinery/pkg/watch"
	//
	// "freepik.com/admitik/internal/template"
)

// TODO: A ver qué hacemos con esto
// processEvent process an event coming from a triggered extra-resource type.
// It reconciles stored resources in an idempotent way
func (r *WatchedResourcesController) processGenerationEvent(resourceType string, eventType watch.EventType, object ...map[string]interface{}) (err error) {

	corelog.Printf("CP0000000000")

	// Process only certain event types
	//if eventType != watch.Added && eventType != watch.Modified && eventType != watch.Deleted {
	//	return nil
	//}
	//
	//if eventType == watch.Deleted {
	//	err = r.Dependencies.SourcesRegistry.RemoveResource(resourceType, &object[0])
	//	return err
	//}
	//
	//// Create/Update events
	//if eventType == watch.Modified {
	//	err = r.Dependencies.SourcesRegistry.RemoveResource(resourceType, &object[0])
	//	if err != nil {
	//		return err
	//	}
	//}
	//
	//r.Dependencies.SourcesRegistry.AddResource(resourceType, &object[0])

	return err
}
