package sources

import (
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TODO
type ResourceTypeName string

// TODO
type ResourceTypeWatcherT struct {
	// Enforce concurrency safety
	Mutex *sync.Mutex

	// Started represents a flag to know if the watcher is running
	Started *bool

	// Blocked represents a flag to prevent watcher from starting
	Blocked *bool

	// StopSignal represents a flag to kill the watcher.
	// Watcher will be potentially re-launched by SourcesController
	StopSignal *chan bool

	// Dependants represents the amount of policies
	// depending on the resources cached by this watcher
	//Dependants int

	//
	ResourceList *[]*unstructured.Unstructured
}

type WatcherPoolT struct {
	// Enforce concurrency safety
	Mutex *sync.Mutex

	Pool map[ResourceTypeName]ResourceTypeWatcherT
}
