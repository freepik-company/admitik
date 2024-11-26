package globals

import (
	"context"
	"sync"

	//

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/sources"
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

	// Kubernetes clients
	KubeRawClient     *dynamic.DynamicClient
	KubeRawCoreClient *kubernetes.Clientset

	//
	SourceController *sources.SourcesController

	//
	ClusterAdmissionPolicyPool ClusterAdmissionPolicyPoolT
}
