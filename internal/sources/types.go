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

package sources

import (
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TODO
type resourceTypeName string

// TODO
type resourceTypeWatcherT struct {
	// Enforce concurrency safety
	Mutex *sync.RWMutex

	// Started represents a flag to know if the watcher is running
	Started *bool

	// Blocked represents a flag to prevent watcher from starting
	Blocked *bool

	// StopSignal represents a flag to kill the watcher.
	// Watcher will be potentially re-launched by SourcesController
	StopSignal *chan bool

	//
	ResourceList []*unstructured.Unstructured
}

type WatcherPoolT struct {
	// Enforce concurrency safety
	Mutex *sync.RWMutex

	Pool map[resourceTypeName]*resourceTypeWatcherT
}
