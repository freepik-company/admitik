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
	"io"
	"net/http"
	"strings"

	//
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/template"
)

// handleValidationRequest handles the incoming validation requests
func (s *HttpServer) handleValidationRequest(response http.ResponseWriter, request *http.Request) {
	logger := log.FromContext(request.Context())

	var err error

	// Read the request
	bodyBytes, err := io.ReadAll(request.Body)
	if err != nil {
		logger.Info(fmt.Sprintf("failed reading request.body: %s", err.Error()))
		return
	}

	//
	requestObj := admissionv1.AdmissionReview{}
	err = json.Unmarshal(bodyBytes, &requestObj)
	if err != nil {
		logger.Info(fmt.Sprintf("failed decoding JSON from request.body: %s", err.Error()))
		return
	}

	// Automatically add some information to the logs
	logger = logger.WithValues(
		"object", fmt.Sprintf("%s/%s/%s/%s",
			requestObj.Request.Resource.Group,
			requestObj.Request.Resource.Version,
			requestObj.Request.Resource.Resource,
			requestObj.Request.Name,
		),
		"namespace", requestObj.Request.Namespace,
		"operation", requestObj.Request.Operation)

	logger.Info("object under validation review")

	// Assume that the request is rejected as default
	reviewResponse := &requestObj
	reviewResponse.Response = &admissionv1.AdmissionResponse{
		UID:     requestObj.Request.UID,
		Allowed: false,
		Result: &metav1.Status{
			Code: http.StatusForbidden,
		},
	}

	defer func() {
		responseBytes, err := json.Marshal(reviewResponse)
		if err != nil {
			logger.Info(fmt.Sprintf("failed converting final response.body into valid JSON: %s", err.Error()))
			return
		}

		response.WriteHeader(http.StatusOK)
		response.Header().Set("Content-Type", "application/json")

		_, err = response.Write(responseBytes)
		if err != nil {
			logger.Info(fmt.Sprintf("failed writing response to the client: %s", err.Error()))
		}
	}()

	// Craft the resourcePattern to look for the ClusterValidationPolicy objects in the pool
	resourcePattern := fmt.Sprintf("%s/%s/%s/%s",
		requestObj.Request.Resource.Group,
		requestObj.Request.Resource.Version,
		requestObj.Request.Resource.Resource,
		requestObj.Request.Operation)

	// Create an object that will be injected in conditions/message
	// in later template evaluation stage
	commonTemplateInjectedObject := template.InjectedDataT{}
	commonTemplateInjectedObject.Initialize()

	// Loop over ClusterValidationPolicy resources performing actions
	// At this point, some extra params will be added to the object that will be injected in template
	caPolicyList := s.dependencies.ClusterValidationPolicyRegistry.GetResources(resourcePattern)

	for _, caPolicyObj := range caPolicyList {

		// Assume rejection for each policy individually
		reviewResponse.Response.Allowed = false

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterValidationPolicy", caPolicyObj.Name)

		// Retrieve the sources declared per policy
		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject.Sources = common.FetchPolicySources(caPolicyObj, s.dependencies.SourcesRegistry)

		// Evaluate template conditions
		conditionsPassed, condErr := common.IsPassingConditions(caPolicyObj.Spec.Conditions, &specificTemplateInjectedObject)
		if condErr != nil {
			logger.Info(fmt.Sprintf("failed evaluating conditions: %s", condErr.Error()))
		}

		// Conditions are met, skip rejection
		if conditionsPassed {
			continue
		}

		// When some condition is not met, evaluate message's template and emit a response
		var parsedMessage string
		parsedMessage, err = template.EvaluateTemplate(caPolicyObj.Spec.Message.Engine, caPolicyObj.Spec.Message.Template, &specificTemplateInjectedObject)
		if err != nil {
			logger.Info(fmt.Sprintf("failed parsing message template: %s", err.Error()))
			parsedMessage = "Reason unavailable: message template failed. More info in controller logs."
		}

		reviewResponse.Response.Result.Message = parsedMessage

		// When the policy is in Permissive mode, allow it anyway
		var kubeEventAction string
		if strings.ToLower(caPolicyObj.Spec.FailureAction) == v1alpha1.ValidationFailureActionPermissive {
			reviewResponse.Response.Allowed = true
			kubeEventAction = "AllowedWithViolations"
			logger.Info(fmt.Sprintf("object accepted with unmet conditions: %s", parsedMessage))
		} else {
			kubeEventAction = "Rejected"
			logger.Info(fmt.Sprintf("object rejected due to unmet conditions: %s", parsedMessage))
		}

		// Create the Event in Kubernetes about involved object
		err = common.CreateKubeEvent(request.Context(), "default", "admission-server",
			commonTemplateInjectedObject.Object, *caPolicyObj, kubeEventAction, parsedMessage)
		if err != nil {
			logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
		}

		// On conditions not being met, first required policy causes early full rejection
		if strings.ToLower(caPolicyObj.Spec.FailureAction) == v1alpha1.ValidationFailureActionEnforce {
			return
		}
	}

	reviewResponse.Response.Allowed = true
	reviewResponse.Response.Result = &metav1.Status{}
}
