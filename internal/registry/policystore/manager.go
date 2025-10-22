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

package policystore

import (
	"reflect"
	"slices"
	"strings"

	//
	"github.com/freepik-company/admitik/internal/pubsub"
	"github.com/freepik-company/admitik/internal/store/simple"
)

func NewPolicyStore[T PolicyResourceI]() *PolicyStore[T] {

	return &PolicyStore[T]{
		Store:       simple.NewStore[T](),
		Broadcaster: pubsub.NewBroadcaster[T](),
	}
}

// GetReferencedSources returns a list of GVR names referenced on 'sources' section across the policies.
// GVR is expressed as {group}/{version}/{resource}
func (ps *PolicyStore[T]) GetReferencedSources() []string {

	sourceTypes := []string{}

	// Loop over all objects collecting extra resources
	tmpCollectionNames := ps.Store.GetCollectionNames()
	for _, tmpCollectionName := range tmpCollectionNames {

		collectionObjectList := ps.Store.GetResources(tmpCollectionName)
		for _, resourceObj := range collectionObjectList {

			for _, source := range resourceObj.GetSources() {

				// Prevents potential explosions due to
				// 'sources' comes empty from time to time
				if reflect.ValueOf(source).IsZero() {
					continue
				}

				sourceName := strings.Join([]string{source.Group, source.Version, source.Resource}, "/")
				sourceTypes = append(sourceTypes, sourceName)
			}
		}
	}

	// Clean duplicated
	slices.Sort(sourceTypes)
	sourceTypes = slices.Compact(sourceTypes)

	return sourceTypes
}
