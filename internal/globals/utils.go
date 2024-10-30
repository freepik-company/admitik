package globals

import (

	//
	"freepik.com/admitik/api/v1alpha1"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	//
)

// NewKubernetesClient return a new Kubernetes Dynamic client from client-go SDK
func NewKubernetesClient() (client *dynamic.DynamicClient, err error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return client, err
	}

	// Create the clients to do requests to our friend: Kubernetes
	client, err = dynamic.NewForConfig(config)
	if err != nil {
		return client, err
	}

	return client, err
}

// TODO
func GetClusterAdmissionPolicyIndex(resourcePattern string, resource *v1alpha1.ClusterAdmissionPolicy) (result int) {

	tmpResourceList := Application.ClusterAdmissionPolicyPool.Pool[resourcePattern]

	for objectIndex, object := range tmpResourceList {
		if object.Name == resource.Name {
			return objectIndex
		}
	}

	return -1
}

// TODO
func GetClusterAdmissionPoolPolicyIndexes(resource *v1alpha1.ClusterAdmissionPolicy) (result map[string]int) {

	result = make(map[string]int)

	for resourcePattern, _ := range Application.ClusterAdmissionPolicyPool.Pool {
		policyIndex := GetClusterAdmissionPolicyIndex(resourcePattern, resource)

		if policyIndex != -1 {
			result[resourcePattern] = policyIndex
		}
	}

	return result
}

// TODO
func CreateClusterAdmissionPoolPolicy(resourcePattern string, resource *v1alpha1.ClusterAdmissionPolicy) {

	tmpResourceList := Application.ClusterAdmissionPolicyPool.Pool[resourcePattern]

	Application.ClusterAdmissionPolicyPool.Mutex.Lock()
	defer Application.ClusterAdmissionPolicyPool.Mutex.Unlock()

	temporaryManifest := (*resource).DeepCopy()
	tmpResourceList = append(tmpResourceList, *temporaryManifest)
	Application.ClusterAdmissionPolicyPool.Pool[resourcePattern] = tmpResourceList
}

// TODO
func DeleteClusterAdmissionPoolPolicyByIndex(resourcePattern string, objectIndex int) {

	policyList := Application.ClusterAdmissionPolicyPool.Pool[resourcePattern]

	Application.ClusterAdmissionPolicyPool.Mutex.Lock()
	defer Application.ClusterAdmissionPolicyPool.Mutex.Unlock()

	// Replace the selected object with the last one from the list,
	// then replace the whole list with it, minus the last.
	policyList = append((policyList)[:objectIndex], (policyList)[objectIndex+1:]...)

	// Clean empty patterns in the pool
	if len(policyList) == 0 {
		delete(Application.ClusterAdmissionPolicyPool.Pool, resourcePattern)
		return
	}

	Application.ClusterAdmissionPolicyPool.Pool[resourcePattern] = policyList
}
