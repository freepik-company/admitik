package globals

import (
	"context"
	"sync"

	"freepik.com/admitik/api/v1alpha1"
)

var (
	Application = applicationT{
		Context: context.Background(),

		ClusterAdmissionPolicyPool: ClusterAdmissionPolicyPoolT{
			Mutex: &sync.Mutex{},
			Pool:  make(map[string][]v1alpha1.ClusterAdmissionPolicy),
		},
	}
)
