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
	admissionV1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FailureActionAudit   string = "Audit"
	FailureActionEnforce string = "Enforce"
)

// WatchedResourceT represents TODO
type WatchedResourceT struct {
	metav1.GroupVersionResource `json:",inline"`

	Operations []admissionV1.OperationType `json:"operations"`
}

type SourceT struct {
	metav1.GroupVersionResource `json:",inline"`

	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type ConditionT struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type MessageT struct {
	Template string `json:"template"`
}

// ClusterAdmissionPolicySpec defines the desired state of ClusterAdmissionPolicy
type ClusterAdmissionPolicySpec struct {
	FailureAction string `json:"failureAction,omitempty"`

	//
	WatchedResources WatchedResourceT `json:"watchedResources"`
	Sources          []SourceT        `json:"sources"`

	//
	Conditions []ConditionT `json:"conditions"`
	Message    MessageT     `json:"message"`
}

// ClusterAdmissionPolicyStatus defines the observed state of ClusterAdmissionPolicy
type ClusterAdmissionPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clusteradmissionpolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterAdmissionPolicy is the Schema for the clusteradmissionpolicies API
type ClusterAdmissionPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterAdmissionPolicySpec   `json:"spec,omitempty"`
	Status ClusterAdmissionPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterAdmissionPolicyList contains a list of ClusterAdmissionPolicy
type ClusterAdmissionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterAdmissionPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterAdmissionPolicy{}, &ClusterAdmissionPolicyList{})
}
