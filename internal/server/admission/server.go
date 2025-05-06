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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	//
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/template"
)

// HttpServer represents a simple HTTP server
type HttpServer struct {
	*http.Server

	// Injected dependencies
	dependencies *AdmissionServerDependencies
}

// NewHttpServer creates a new HttpServer
func NewHttpServer(dependencies *AdmissionServerDependencies) *HttpServer {
	return &HttpServer{
		&http.Server{},
		dependencies,
	}
}

// setAddr sets the address for the server
func (s *HttpServer) setAddr(addr string) {
	s.Server.Addr = addr
}

// setHandler sets the handler for the server
func (s *HttpServer) setHandler(handler http.Handler) {
	s.Server.Handler = handler
}

// handleRequest handles the incoming requests
func (s *HttpServer) handleRequest(response http.ResponseWriter, request *http.Request) {
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

	logger.Info("object under review")

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

	// Craft the resourcePattern to look for the ClusterAdmissionPolicy objects in the pool
	resourcePattern := fmt.Sprintf("%s/%s/%s/%s",
		requestObj.Request.Resource.Group,
		requestObj.Request.Resource.Version,
		requestObj.Request.Resource.Resource,
		requestObj.Request.Operation)

	// Create an object that will be injected in conditions/message
	// in later Golang's template evaluation stage
	commonTemplateInjectedObject := map[string]interface{}{}
	commonTemplateInjectedObject, err = s.extractAdmissionRequestData(&requestObj)
	if err != nil {
		logger.Info(fmt.Sprintf("failed extracting data from AdmissionReview: %s", err.Error()))
		return
	}

	//
	requestObject, ok := commonTemplateInjectedObject["object"].(map[string]interface{})
	if !ok {
		logger.Info("failed converting types for presented resource")
		return
	}

	// Loop over ClusterAdmissionPolicies performing actions
	// At this point, some extra params will be added to the object that will be injected in template
	caPolicyList := s.dependencies.ClusterAdmissionPoliciesRegistry.GetResources(resourcePattern)

	for _, caPolicyObj := range caPolicyList {

		// Assume rejection for each policy individually
		reviewResponse.Response.Allowed = false

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterAdmissionPolicy", caPolicyObj.Name)

		// Retrieve the sources declared per policy
		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject["sources"] = s.fetchPolicySources(caPolicyObj)

		// Evaluate template conditions
		var conditionsPassedList []bool
		for _, condition := range caPolicyObj.Spec.Conditions {

			var conditionPassed bool
			var condErr error

			// Choose between gotmpl or starlark
			if condition.Engine == v1alpha1.TemplateEngineStarlark {
				conditionPassed, condErr = s.isPassingStarlarkCondition(condition.Key, condition.Value, &specificTemplateInjectedObject)
			} else {
				conditionPassed, condErr = s.isPassingGotmplCondition(condition.Key, condition.Value, &specificTemplateInjectedObject)
			}

			if condErr != nil {
				logger.Info(fmt.Sprintf("failed evaluating condition '%s': %s", condition.Name, condErr.Error()))
				conditionsPassedList = append(conditionsPassedList, false)
				continue
			}

			conditionsPassedList = append(conditionsPassedList, conditionPassed)
		}

		// When some condition is not met, evaluate message's template and emit a response
		if slices.Contains(conditionsPassedList, false) {
			var parsedMessage string
			if caPolicyObj.Spec.Message.Engine == v1alpha1.TemplateEngineStarlark {
				parsedMessage, err = template.EvaluateTemplateStarlark(caPolicyObj.Spec.Message.Template, &specificTemplateInjectedObject)
			} else {
				parsedMessage, err = template.EvaluateTemplate(caPolicyObj.Spec.Message.Template, &specificTemplateInjectedObject)
			}

			if err != nil {
				logger.Info(fmt.Sprintf("failed parsing message template: %s", err.Error()))
				parsedMessage = "Reason unavailable: message template failed. More info in controller logs."
			}

			reviewResponse.Response.Result.Message = parsedMessage

			// When the policy is in Permissive mode, allow it anyway
			var kubeEventAction string
			if caPolicyObj.Spec.FailureAction == v1alpha1.FailureActionPermissive {
				reviewResponse.Response.Allowed = true
				kubeEventAction = "AllowedWithViolations"
				logger.Info(fmt.Sprintf("object accepted with unmet conditions: %s", parsedMessage))
			} else {
				kubeEventAction = "Rejected"
				logger.Info(fmt.Sprintf("object rejected due to unmet conditions: %s", parsedMessage))
			}

			// Create the Event in Kubernetes about involved object
			err = createKubeEvent(request.Context(), "default", requestObject, *caPolicyObj, kubeEventAction, parsedMessage)
			if err != nil {
				logger.Info(fmt.Sprintf("failed creating kubernetes event: %s", err.Error()))
			}

			// On conditions not being met, first required policy causes early full rejection
			if caPolicyObj.Spec.FailureAction == v1alpha1.FailureActionEnforce {
				return
			}
		}
	}

	reviewResponse.Response.Allowed = true
	reviewResponse.Response.Result = &metav1.Status{}
}

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

// getObjectSources TODO
func (s *HttpServer) fetchPolicySources(caPolicyObj *v1alpha1.ClusterAdmissionPolicy) (results map[int][]map[string]any) {

	results = make(map[int][]map[string]any)

	for sourceIndex, sourceItem := range caPolicyObj.Spec.Sources {

		sourceString := fmt.Sprintf("%s/%s/%s/%s/%s",
			sourceItem.Group,
			sourceItem.Version,
			sourceItem.Resource,
			sourceItem.Namespace,
			sourceItem.Name)

		sourceObjList := s.dependencies.SourcesRegistry.GetResources(sourceString)

		// Store obtained sources in the results map
		for _, itemObj := range sourceObjList {
			results[sourceIndex] = append(results[sourceIndex], *itemObj)
		}
	}

	return results
}

// isPassingStarlarkCondition returns equality between the result given by the 'key' Starlak template and the 'value'
// For template evaluation, it injects extra information available to the user
func (s *HttpServer) isPassingStarlarkCondition(key, value string, injectedValues *map[string]interface{}) (result bool, err error) {

	parsedKey, err := template.EvaluateTemplateStarlark(key, injectedValues)
	if err != nil {
		return false, err
	}

	return parsedKey == value, nil
}

// isPassingGotmplCondition returns equality between the result given by the 'key' Gotmpl template and the 'value'
// For template evaluation, it injects extra information available to the user
func (s *HttpServer) isPassingGotmplCondition(key, value string, injectedValues *map[string]interface{}) (bool, error) {

	parsedKey, err := template.EvaluateTemplate(key, injectedValues)
	if err != nil {
		return false, err
	}

	return parsedKey == value, nil
}

// createKubeEvent creates a modern event in Kuvernetes with data given by params
func createKubeEvent(ctx context.Context, namespace string, object map[string]interface{},
	policy v1alpha1.ClusterAdmissionPolicy, action, message string) (err error) {

	objectData, err := globals.GetObjectBasicData(&object)
	if err != nil {
		return err
	}

	eventObj := eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "admission-",
		},

		EventTime:           metav1.NewMicroTime(time.Now()),
		ReportingController: "admitik",
		ReportingInstance:   "admission-server",
		Action:              action,
		Reason:              "ClusterAdmissionPolicyAudit",

		Regarding: corev1.ObjectReference{
			APIVersion: objectData["apiVersion"].(string),
			Kind:       objectData["kind"].(string),
			Name:       objectData["name"].(string),
			Namespace:  objectData["namespace"].(string),
		},

		Related: &corev1.ObjectReference{
			APIVersion: policy.APIVersion,
			Kind:       policy.Kind,
			Name:       policy.Name,
		},

		Note: message,
		Type: "Normal",
	}

	_, err = globals.Application.KubeRawCoreClient.EventsV1().Events(namespace).
		Create(ctx, &eventObj, metav1.CreateOptions{})

	return err
}
