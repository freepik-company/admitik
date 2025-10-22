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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MutationPatchTypeJson           string = "jsonpatch"
	MutationPatchTypeMerge          string = "jsonmerge"
	MutationPatchTypeStrategicMerge string = "strategicmerge"
)

type PatchT struct {
	Type     string `json:"type"`
	Engine   string `json:"engine,omitempty"`
	Template string `json:"template"`
}

// ClusterMutationPolicySpec defines the desired state of ClusterMutationPolicy
type ClusterMutationPolicySpec struct {

	// Priority represents the execution order of the policy.
	// Policies with higher values are evaluated later.
	Priority int `json:"priority,omitempty"`

	// InterceptedResources represents a list of resource-groups that will be sent to the admissions server to be evaluated
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	InterceptedResources []AdmissionResourceGroupT `json:"interceptedResources"`

	// Sources represents a list of extra resource-groups to watch and inject in templates
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	Sources []SourceGroupT `json:"sources"`

	// Conditions represents a list of conditions that must be passed to meet the policy
	// +listType=map
	// +listMapKey=name
	Conditions []ConditionT `json:"conditions"`

	Patch PatchT `json:"patch"`
}

// ClusterMutationPolicyStatus defines the observed state of ClusterMutationPolicy
type ClusterMutationPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clustermutationpolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterMutationPolicy is the Schema for the clustermutationpolicies API
type ClusterMutationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterMutationPolicySpec   `json:"spec,omitempty"`
	Status ClusterMutationPolicyStatus `json:"status,omitempty"`
}

func (p *ClusterMutationPolicy) GetName() string {
	return p.Name
}

func (p *ClusterMutationPolicy) GetSources() []SourceGroupT {
	return p.Spec.Sources
}

// +kubebuilder:object:root=true

// ClusterMutationPolicyList contains a list of ClusterMutationPolicy
type ClusterMutationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterMutationPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterMutationPolicy{}, &ClusterMutationPolicyList{})
}
