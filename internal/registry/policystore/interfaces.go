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

package policystore

import (
	"time"

	"github.com/freepik-company/admitik/api/v1alpha1"
)

// PolicyResourceI represents the minimal contract that all policy types must fulfill
// to participate in the policy registry.
type PolicyResourceI interface {
	GetName() string
	GetPolicyKind() string
	GetSources() []v1alpha1.SourceGroupT
	GetConditions() []v1alpha1.ConditionT

	// GetConditionRecheckInterval returns the interval at which the policy's conditions
	// should be re-evaluated even without a watched-resource event. A zero duration means
	// no periodic recheck is desired.
	GetConditionRecheckInterval() time.Duration
}
