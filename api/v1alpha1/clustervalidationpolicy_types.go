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
	ValidationFailureActionPermissive string = "permissive"
	ValidationFailureActionEnforce    string = "enforce"
)

type MessageT struct {
	Engine   string `json:"engine,omitempty"`
	Template string `json:"template"`
}

// ClusterValidationPolicySpec defines the desired state of ClusterValidationPolicy
type ClusterValidationPolicySpec struct {
	FailureAction string `json:"failureAction,omitempty"`

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
	// +listMapKey=name
	// +listMapKey=namespace
	Sources []ResourceGroupT `json:"sources"`

	// Conditions represents a list of conditions that must be passed to meet the policy
	// +listType=map
	// +listMapKey=name
	Conditions []ConditionT `json:"conditions"`

	Message MessageT `json:"message"`
}

// ClusterValidationPolicyStatus defines the observed state of ClusterValidationPolicy
type ClusterValidationPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clustervalidationpolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterValidationPolicy is the Schema for the clustervalidationpolicies API
type ClusterValidationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterValidationPolicySpec   `json:"spec,omitempty"`
	Status ClusterValidationPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterValidationPolicyList contains a list of ClusterValidationPolicy
type ClusterValidationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterValidationPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterValidationPolicy{}, &ClusterValidationPolicyList{})
}
