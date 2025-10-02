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

type SourceGroupFiltersRegexT struct {
	Negative   bool   `json:"negative"`
	Expression string `json:"expression"`
}

type SourceGroupFiltersNamespaceT struct {
	MatchList  []string                  `json:"matchList,omitempty"`
	MatchRegex *SourceGroupFiltersRegexT `json:"matchRegex,omitempty"`
}

type SourceGroupFiltersNameT struct {
	MatchList  []string                  `json:"matchList,omitempty"`
	MatchRegex *SourceGroupFiltersRegexT `json:"matchRegex,omitempty"`
}

type SourceGroupFiltersMetadataT struct {
	MatchLabels      map[string]string `json:"matchLabels,omitempty"`
	MatchAnnotations map[string]string `json:"matchAnnotations,omitempty"`
}

type SourceGroupFiltersT struct {
	Namespace *SourceGroupFiltersNamespaceT `json:"namespace,omitempty"`
	Name      *SourceGroupFiltersNameT      `json:"name,omitempty"`
	Metadata  *SourceGroupFiltersMetadataT  `json:"metadata,omitempty"`
}

// SourceGroupT represents a TODO
type SourceGroupT struct {
	metav1.GroupVersionResource `json:",inline"`

	Filters *SourceGroupFiltersT `json:"filters,omitempty"`
}

// ResourceGroupT represents a resource-group that will be watched to be evaluated
type ResourceGroupT struct {
	metav1.GroupVersionResource `json:",inline"`

	// +default=""
	// +kubebuilder:default=""
	Name string `json:"name,omitempty"`

	// +default=""
	// +kubebuilder:default=""
	Namespace string `json:"namespace,omitempty"`
}

// AdmissionResourceGroupT represents a resource-group that will be sent to the admissions server to be evaluated
type AdmissionResourceGroupT struct {
	metav1.GroupVersionResource `json:",inline"`

	// Conditions represents a list of conditions that must be passed to meet the policy
	// +listType=set
	Operations []admissionV1.OperationType `json:"operations"`
}

// ConditionT represents a condition that must be passed to meet the policy
type ConditionT struct {
	Name   string `json:"name"`
	Engine string `json:"engine,omitempty"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}
