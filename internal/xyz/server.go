package xyz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/template"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// HttpServer represents a simple HTTP server
type HttpServer struct {
	*http.Server
}

// NewHttpServer creates a new HttpServer
func NewHttpServer() *HttpServer {
	return &HttpServer{
		&http.Server{},
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

	// Assume that the request is rejected as default
	reviewResponse := &requestObj
	reviewResponse.Response = &admissionv1.AdmissionResponse{
		UID:     requestObj.Request.UID,
		Allowed: false,
		Result: &v1.Status{
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
	resourcePattern := fmt.Sprintf("/%s/%s/%s/%s",
		requestObj.Request.Resource.Group,
		requestObj.Request.Resource.Version,
		requestObj.Request.Resource.Resource,
		requestObj.Request.Operation)

	// Create an object that will be injected in conditions/message
	// in later Golang's template evaluation stage
	commonTemplateInjectedObject := map[string]interface{}{}
	commonTemplateInjectedObject["operation"] = string(requestObj.Request.Operation)

	//
	requestObject := map[string]interface{}{}
	err = json.Unmarshal(requestObj.Request.Object.Raw, &requestObject)
	if err != nil {
		logger.Info(fmt.Sprintf("failed decoding JSON from AdmissionReview field 'request.object': %s", err.Error()))
		return
	}
	commonTemplateInjectedObject["object"] = requestObject

	//
	if requestObj.Request.Operation == admissionv1.Update {
		requestOldObject := map[string]interface{}{}
		err = json.Unmarshal(requestObj.Request.OldObject.Raw, &requestOldObject)
		if err != nil {
			logger.Info(fmt.Sprintf("failed decoding JSON from AdmissionReview field 'request.oldObject': %s", err.Error()))
			return
		}
		commonTemplateInjectedObject["oldObject"] = requestOldObject
	}

	// Loop over ClusterAdmissionPolicies performing actions
	// At this point, some extra params will be added to the object that will be injected in template
	for _, caPolicyObj := range globals.Application.ClusterAdmissionPolicyPool.Pool[resourcePattern] {

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterAdmissionPolicy", caPolicyObj.Name)

		//
		specificTemplateInjectedObject := commonTemplateInjectedObject

		// Retrieve the sources declared per policy
		for sourceIndex, sourceItem := range caPolicyObj.Spec.Sources {

			unstructuredSourceObjList, err := getKubeResourceList(request.Context(),
				sourceItem.Group,
				sourceItem.Version,
				sourceItem.Resource,
				sourceItem.Namespace,
				sourceItem.Name)

			if err != nil {
				logger.Info(fmt.Sprintf("failed getting sources: %s", err.Error()))
				return
			}

			// Store obtained sources as a map[int][]map[string]interface{}
			tmpSources, ok := specificTemplateInjectedObject["sources"].(map[int][]map[string]interface{})
			if !ok {
				tmpSources = make(map[int][]map[string]interface{})
			}

			for _, unstructuredItem := range unstructuredSourceObjList.Items {
				tmpSources[sourceIndex] = append(tmpSources[sourceIndex], unstructuredItem.Object)
			}

			specificTemplateInjectedObject["sources"] = tmpSources
		}

		// Evaluate template conditions
		var conditionPassed []bool
		for _, condition := range caPolicyObj.Spec.Conditions {
			parsedKey, err := template.EvaluateTemplate(condition.Key, &specificTemplateInjectedObject)
			if err != nil {
				logger.Info(fmt.Sprintf("failed evaluating condition '%s': %s", condition.Name, err.Error()))
				conditionPassed = append(conditionPassed, false)
				continue
			}

			conditionPassed = append(conditionPassed, parsedKey == condition.Value)
		}

		// When some condition is not met, evaluate message's template and emit a negative response
		if slices.Contains(conditionPassed, false) {
			parsedMessage, err := template.EvaluateTemplate(caPolicyObj.Spec.Message.Template, &specificTemplateInjectedObject)
			if err != nil {
				logger.Info(fmt.Sprintf("failed parsing message template: %s", err.Error()))
				return
			}

			logger.Info(fmt.Sprintf("object rejected due to unmet conditions: %s", parsedMessage))
			reviewResponse.Response.Result.Message = parsedMessage
			return
		}
	}

	reviewResponse.Response.Allowed = true
	reviewResponse.Response.Result = &v1.Status{}
}

// getKubeResourceList returns an unstructuredList of resources selected by params
func getKubeResourceList(ctx context.Context, group, version, resource, namespace, name string) (
	resourceList *unstructured.UnstructuredList, err error) {

	unstructuredSourceObj := globals.Application.KubeRawClient.Resource(schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	})

	sourceListOptions := v1.ListOptions{}

	if namespace != "" {
		sourceListOptions.FieldSelector = fmt.Sprintf("metadata.namespace=%s", namespace)
	}

	if name != "" {
		if sourceListOptions.FieldSelector != "" {
			sourceListOptions.FieldSelector += ","
		}
		sourceListOptions.FieldSelector = fmt.Sprintf("metadata.name=%s", name)
	}

	resourceList, err = unstructuredSourceObj.List(ctx, sourceListOptions)
	return resourceList, err
}
