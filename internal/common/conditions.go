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

package common

import (
	"fmt"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/template"
)

// IsPassingConditions iterate over a list of templated conditions and return whether they are passing or not
func IsPassingConditions(conditionList []v1alpha1.ConditionT, injectedData *template.ConditionsInjectedDataT) (result bool, err error) {
	for _, condition := range conditionList {

		// Choose templating engine. Maybe more will be added in the future
		parsedKey, condErr := template.EvaluateTemplate(condition.Engine, condition.Key, injectedData)
		if condErr != nil {
			return false, fmt.Errorf("failed condition '%s': %s", condition.Name, condErr.Error())
		}

		if parsedKey != condition.Value {
			return false, nil
		}
	}

	return true, nil
}
