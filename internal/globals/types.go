package globals

import (
	"context"
	"sync"

	//
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	//
	"freepik.com/admitik/api/v1alpha1"
)

// TODO
// type ResourceTypeWatcherT struct {
// 	// Enforce concurrency safety
// 	Mutex *sync.Mutex

// 	// Started represents a flag to know if the watcher is running
// 	Started *bool
// 	// Blocked represents a flag to prevent watcher from starting
// 	Blocked *bool
// 	// StopSignal represents a flag to kill the watcher.
// 	// Watcher will be potentially re-launched by xyz.WorkloadController
// 	StopSignal *chan bool

// type AdmissionPolicyPoolT struct {
// 	// Enforce concurrency safety
// 	Mutex *sync.Mutex

// 	Pool []v1alpha1.AdmissionPolicy
// }

type ClusterAdmissionPolicyPoolT struct {
	// Enforce concurrency safety
	Mutex *sync.Mutex

	Pool map[string][]v1alpha1.ClusterAdmissionPolicy
}

// type AdmissionPoolT struct {
// 	// Enforce concurrency safety
// 	Mutex *sync.Mutex

// 	Pool map[string][]v1alpha1.ClusterAdmissionPolicy
// }

// ApplicationT TODO
type applicationT struct {
	// Context TODO
	Context context.Context

	// KubeRawClient TODO
	KubeRawClient *dynamic.DynamicClient

	// KubeRawCoreClient TODO
	KubeRawCoreClient *kubernetes.Clientset

	//
	ClusterAdmissionPolicyPool ClusterAdmissionPolicyPoolT
	//AdmissionPolicyPool AdmissionPolicyPoolT

	// WatcherPool TODO
	//AdmissionPoolT map[string]AdmissionPoolT
	//ClasifiedAdmissionPolicyPool AdmissionPoolT
}
