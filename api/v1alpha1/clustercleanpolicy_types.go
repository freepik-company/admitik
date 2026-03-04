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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterCleanPolicySpec defines the desired state of ClusterCleanPolicy
type ClusterCleanPolicySpec struct {

	// ConditionRecheckInterval defines how often the policy conditions are re-evaluated
	// against the watched resource, even when no change event has been received.
	// When zero (default), no periodic recheck is performed.
	ConditionRecheckInterval metav1.Duration `json:"conditionRecheckInterval,omitempty"`

	// WatchedResources represents a list of resource-groups that will be watched to be evaluated
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	// +listMapKey=name
	// +listMapKey=namespace
	WatchedResources []ResourceGroupT `json:"watchedResources"`

	// Sources represents a list of extra resource-groups to watch and inject in templates
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	Sources []SourceGroupT `json:"sources"`

	// Conditions represents a list of conditions that must be passed to trigger the clean action
	// +listType=map
	// +listMapKey=name
	Conditions []ConditionT `json:"conditions"`

	// Target defines the resource to delete when conditions are met
	Target CleanTargetT `json:"target"`
}

// CleanTargetT defines the target resource to be cleaned
type CleanTargetT struct {
	Engine   string `json:"engine,omitempty"`
	Template string `json:"template"`
}

// ClusterCleanPolicyStatus defines the observed state of ClusterCleanPolicy
type ClusterCleanPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clustercleanpolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterCleanPolicy is the Schema for the clustercleanpolicies API
type ClusterCleanPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterCleanPolicySpec   `json:"spec,omitempty"`
	Status ClusterCleanPolicyStatus `json:"status,omitempty"`
}

func (p *ClusterCleanPolicy) GetName() string {
	return p.Name
}

func (p *ClusterCleanPolicy) GetSources() []SourceGroupT {
	return p.Spec.Sources
}

func (p *ClusterCleanPolicy) GetConditions() []ConditionT {
	return p.Spec.Conditions
}

// GetConditionRecheckInterval returns the interval at which conditions should be
// re-evaluated periodically, independent of watched-resource events.
func (p *ClusterCleanPolicy) GetConditionRecheckInterval() time.Duration {
	return p.Spec.ConditionRecheckInterval.Duration
}

// +kubebuilder:object:root=true

// ClusterCleanPolicyList contains a list of ClusterCleanPolicy
type ClusterCleanPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterCleanPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterCleanPolicy{}, &ClusterCleanPolicyList{})
}
