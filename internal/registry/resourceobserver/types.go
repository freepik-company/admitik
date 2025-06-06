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

package resourceobserver

import "sync"

// ResourceTypeName represents TODO
// The pattern will be: {group}/{version}/{resource}/{namespace}/{name}
type ResourceTypeName = string

// ResourceObserverGroup wraps status of a group of observers.
type ResourceObserverGroup struct {
	mu sync.Mutex

	// observers represents the audience for a group of resources
	observers []string
}

// ResourceObserverRegistry manage observers
type ResourceObserverRegistry struct {
	mu        sync.Mutex
	observers map[ResourceTypeName]*ResourceObserverGroup
}
