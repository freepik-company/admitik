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

// ObjectCloneT TODO: Future capability
type ObjectCloneT struct {
}

// ObjectDefinitionT holds the engine and template used to render the generated resource.
type ObjectDefinitionT struct {
	Engine   string `json:"engine,omitempty"`
	Template string `json:"template"`
}

// ObjectT groups the clone (future) and definition configuration for the generated object.
type ObjectT struct {
	Clone      ObjectCloneT      `json:"clone"`
	Definition ObjectDefinitionT `json:"definition"`
}

// ClusterGenerationPolicySpec defines the desired state of ClusterGenerationPolicy
type ClusterGenerationPolicySpec struct {
	// OverwriteExisting controls whether an already-existing generated object will be
	// updated (via server-side apply) when conditions are met again.
	OverwriteExisting bool `json:"overwriteExisting,omitempty"`

	// DeleteOnConditionFalse controls whether the generated object is deleted when
	// conditions are evaluated and found to be false. When false (default), the
	// generated object is left in place even if conditions stop being met.
	DeleteOnConditionFalse bool `json:"deleteOnConditionFalse,omitempty"`

	// ConditionRecheckInterval defines how often the policy conditions are re-evaluated
	// against the watched resource, even when no change event has been received.
	// This covers cases where conditions depend on sources or external state that changed
	// without triggering a watched-resource event.
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

	// Conditions represents a list of conditions that must be passed to meet the policy
	// +listType=map
	// +listMapKey=name
	Conditions []ConditionT `json:"conditions"`

	Object ObjectT `json:"object"`
}

// ClusterGenerationPolicyStatus defines the observed state of ClusterGenerationPolicy
type ClusterGenerationPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clustergenerationpolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterGenerationPolicy is the Schema for the clustergenerationpolicies API
type ClusterGenerationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterGenerationPolicySpec   `json:"spec,omitempty"`
	Status ClusterGenerationPolicyStatus `json:"status,omitempty"`
}

func (p *ClusterGenerationPolicy) GetName() string {
	return p.Name
}

func (p *ClusterGenerationPolicy) GetSources() []SourceGroupT {
	return p.Spec.Sources
}

// GetConditionRecheckInterval returns the interval at which conditions should be
// re-evaluated periodically, independent of watched-resource events.
func (p *ClusterGenerationPolicy) GetConditionRecheckInterval() time.Duration {
	return p.Spec.ConditionRecheckInterval.Duration
}

// +kubebuilder:object:root=true

// ClusterGenerationPolicyList contains a list of ClusterGenerationPolicy
type ClusterGenerationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGenerationPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterGenerationPolicy{}, &ClusterGenerationPolicyList{})
}
