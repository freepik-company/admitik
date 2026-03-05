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

// CloneTargetNamespaceSelectorT selects namespaces for cloning using one or more criteria.
// When multiple selectors are specified within the same entry, they are ANDed together.
type CloneTargetNamespaceSelectorT struct {
	// Names is an explicit list of namespace names to clone into.
	// +optional
	Names []string `json:"names,omitempty"`

	// LabelSelector selects namespaces by their labels.
	// Uses the standard Kubernetes LabelSelector (matchLabels + matchExpressions).
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// AnnotationSelector selects namespaces by their annotations.
	// All entries must match (AND semantics).
	// +optional
	AnnotationSelector map[string]string `json:"annotationSelector,omitempty"`
}

// CloneTargetT defines one target selector entry for the clone policy.
// Each entry selects a set of namespaces where watched objects will be cloned.
type CloneTargetT struct {
	// Namespace defines how to select the target namespaces for this entry.
	Namespace CloneTargetNamespaceSelectorT `json:"namespace"`
}

// ClusterClonePolicySpec defines the desired state of ClusterClonePolicy
type ClusterClonePolicySpec struct {
	// OverwriteExisting controls whether an already-existing cloned object in the
	// target namespace will be updated (via server-side apply). When false (default),
	// the clone is skipped if the object already exists.
	OverwriteExisting bool `json:"overwriteExisting,omitempty"`

	// DeleteOnConditionFalse controls whether cloned objects are deleted when
	// conditions are evaluated and found to be false. When false (default), cloned
	// objects are left in place even if conditions stop being met.
	DeleteOnConditionFalse bool `json:"deleteOnConditionFalse,omitempty"`

	// ConditionRecheckInterval defines how often the policy conditions are re-evaluated
	// against the watched resource, even when no change event has been received.
	// When zero (default), no periodic recheck is performed.
	ConditionRecheckInterval metav1.Duration `json:"conditionRecheckInterval,omitempty"`

	// EventMode controls which Kubernetes events this policy emits.
	// "All" (default) emits both success and failure events, "Errors" emits only
	// failure/warning events, and "None" disables event emission entirely.
	// +kubebuilder:default="All"
	EventMode EventMode `json:"eventMode,omitempty"`

	// WatchedResources represents a list of resource-groups that will be watched and cloned
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	// +listMapKey=name
	// +listMapKey=namespace
	WatchedResources []ResourceGroupT `json:"watchedResources"`

	// Conditions represents a list of conditions that must be passed to trigger cloning
	// +listType=map
	// +listMapKey=name
	Conditions []ConditionT `json:"conditions"`

	// Target is a list of namespace selectors defining where watched objects will be cloned.
	// Multiple entries are ORed: a namespace matching any entry is a target.
	// Within a single entry, selectors are ANDed: the namespace must satisfy all specified criteria.
	// The source namespace of the watched object is automatically excluded.
	Target []CloneTargetT `json:"target"`
}

// ClusterClonePolicyStatus defines the observed state of ClusterClonePolicy
type ClusterClonePolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clusterclonepolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterClonePolicy is the Schema for the clusterclonepolicies API
type ClusterClonePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterClonePolicySpec   `json:"spec,omitempty"`
	Status ClusterClonePolicyStatus `json:"status,omitempty"`
}

func (p *ClusterClonePolicy) GetName() string {
	return p.Name
}

func (p *ClusterClonePolicy) GetPolicyKind() string {
	return "ClusterClonePolicy"
}

func (p *ClusterClonePolicy) GetSources() []SourceGroupT {
	return nil
}

func (p *ClusterClonePolicy) GetConditions() []ConditionT {
	return p.Spec.Conditions
}

func (p *ClusterClonePolicy) GetEventMode() EventMode {
	return p.Spec.EventMode
}

func (p *ClusterClonePolicy) GetConditionRecheckInterval() time.Duration {
	return p.Spec.ConditionRecheckInterval.Duration
}

// +kubebuilder:object:root=true

// ClusterClonePolicyList contains a list of ClusterClonePolicy
type ClusterClonePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterClonePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterClonePolicy{}, &ClusterClonePolicyList{})
}
