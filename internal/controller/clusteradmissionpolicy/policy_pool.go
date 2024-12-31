package clusteradmissionpolicy

import "freepik.com/admitik/api/v1alpha1"

// TODO
func (p *ClusterAdmissionPolicyPoolT) getClusterAdmissionPolicyIndex(resourcePattern string, resource *v1alpha1.ClusterAdmissionPolicy) (result int) {

	tmpResourceList := p.Pool[resourcePattern]

	for objectIndex, object := range tmpResourceList {
		if object.Name == resource.Name {
			return objectIndex
		}
	}

	return -1
}

// TODO
func (p *ClusterAdmissionPolicyPoolT) getClusterAdmissionPoolPolicyIndexes(resource *v1alpha1.ClusterAdmissionPolicy) (result map[string]int) {

	result = make(map[string]int)

	for resourcePattern, _ := range p.Pool {
		policyIndex := p.getClusterAdmissionPolicyIndex(resourcePattern, resource)

		if policyIndex != -1 {
			result[resourcePattern] = policyIndex
		}
	}

	return result
}

// TODO
func (p *ClusterAdmissionPolicyPoolT) createClusterAdmissionPoolPolicy(resourcePattern string, resource *v1alpha1.ClusterAdmissionPolicy) {

	tmpResourceList := p.Pool[resourcePattern]

	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	temporaryManifest := (*resource).DeepCopy()
	tmpResourceList = append(tmpResourceList, *temporaryManifest)
	p.Pool[resourcePattern] = tmpResourceList
}

// TODO
func (p *ClusterAdmissionPolicyPoolT) deleteClusterAdmissionPoolPolicyByIndex(resourcePattern string, objectIndex int) {

	policyList := p.Pool[resourcePattern]

	p.Mutex.Lock()
	defer p.Mutex.Unlock()

	// Replace the selected object with the last one from the list,
	// then replace the whole list with it, minus the last.
	policyList = append((policyList)[:objectIndex], (policyList)[objectIndex+1:]...)

	// Clean empty patterns in the pool
	if len(policyList) == 0 {
		delete(p.Pool, resourcePattern)
		return
	}

	p.Pool[resourcePattern] = policyList
}
