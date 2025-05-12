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

	//
	TemplateEnginePlain    string = "plain"
	TemplateEngineCel      string = "cel"
	TemplateEngineStarlark string = "starlark"
	TemplateEngineGotmpl   string = "gotmpl"
)

// WatchedResourceT represents a group of resources being watched
// for admission operations
type WatchedResourceT struct {
	metav1.GroupVersionResource `json:",inline"`

	Operations []admissionV1.OperationType `json:"operations"`
}

// SourceT represents a group of sources being watched
// for later injection
type SourceT struct {
	metav1.GroupVersionResource `json:",inline"`

	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// ConditionT represents a canonical Kubernetes status condition for a resource
type ConditionT struct {
	Name   string `json:"name"`
	Engine string `json:"engine,omitempty"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}
