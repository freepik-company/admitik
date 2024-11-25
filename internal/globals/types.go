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

// ClusterAdmissionPolicyPoolT represents TODO
type ClusterAdmissionPolicyPoolT struct {
	// Enforce concurrency safety
	Mutex *sync.Mutex

	Pool map[string][]v1alpha1.ClusterAdmissionPolicy
}

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

	// WatcherPool TODO
	//WatcherPool map[ResourceTypeName]ResourceTypeWatcherT
	//WatcherPool *sources.WatcherPoolT
}
