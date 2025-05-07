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
	MutationPatchTypeJson  string = "jsonpatch"
	MutationPatchTypeMerge string = "strategicmerge"
)

type PatchT struct {
	Type     string `json:"type"`
	Engine   string `json:"engine,omitempty"`
	Template string `json:"template"`
}

// ClusterMutationPolicySpec defines the desired state of ClusterMutationPolicy
type ClusterMutationPolicySpec struct {
	//
	WatchedResources WatchedResourceT `json:"watchedResources"`
	Sources          []SourceT        `json:"sources"`

	//
	Conditions []ConditionT `json:"conditions"`
	Patch      PatchT       `json:"patch"`
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
