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

package admission

import (
	"encoding/json"
	"fmt"
	//
	admissionv1 "k8s.io/api/admission/v1"
)

// extractAdmissionRequestData TODO
func (s *HttpServer) extractAdmissionRequestData(adReview *admissionv1.AdmissionReview) (results map[string]interface{}, err error) {

	results = make(map[string]interface{})

	// Store desired operation
	results["operation"] = string(adReview.Request.Operation)

	// Store the previous object on updates
	if adReview.Request.Operation == admissionv1.Update {
		requestOldObject := map[string]interface{}{}
		err = json.Unmarshal(adReview.Request.OldObject.Raw, &requestOldObject)
		if err != nil {
			return results, fmt.Errorf("failed decoding JSON field 'request.oldObject': %s", err.Error())
		}
		results["oldObject"] = requestOldObject
	}

	// Store the object that is being touched
	requestObject := map[string]interface{}{}
	err = json.Unmarshal(adReview.Request.Object.Raw, &requestObject)
	if err != nil {
		return results, fmt.Errorf("failed decoding JSON field 'request.object': %s", err.Error())
	}
	results["object"] = requestObject

	return results, nil
}
