package xyz

import (
	"encoding/json"
	"fmt"
	"io"
	coreLog "log"
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
	_ = logger

	// Read the request
	bodyBytes, err := io.ReadAll(request.Body)
	if err != nil {
		coreLog.Print("i have an dream...")
	}

	coreLog.Print(string(bodyBytes))

	//
	requestObj := admissionv1.AdmissionReview{}
	err = json.Unmarshal(bodyBytes, &requestObj)
	if err != nil {
		coreLog.Printf("a dream where people...: %s", err.Error())
	}

	// Craft the resourcePattern to look for the ClusterAdmissionPolicy objects in the pool
	resourcePattern := fmt.Sprintf("/%s/%s/%s/%s",
		requestObj.Request.Resource.Group,
		requestObj.Request.Resource.Version,
		requestObj.Request.Resource.Resource,
		requestObj.Request.Operation)

	// Create the object that will be injected on
	// ClusterAdmissionPolicy conditions/message on Golang template evaluation stage
	commonTemplateInjectedObject := map[string]interface{}{}

	commonTemplateInjectedObject["operation"] = string(requestObj.Request.Operation)
	commonTemplateInjectedObject["object"], err = GetObjectMapFromRuntimeObject(&requestObj.Request.Object.Object)
	if err != nil {
		coreLog.Printf("was it yellow?... : %s", err.Error())
	}

	coreLog.Print(commonTemplateInjectedObject["object"]) ////////////////////////

	coreLog.Printf("coso: %v", requestObj.Request.Object.Object)

	if requestObj.Request.Operation == admissionv1.Update {
		commonTemplateInjectedObject["oldObject"], err = GetObjectMapFromRuntimeObject(&requestObj.Request.OldObject.Object)
		if err != nil {
			coreLog.Printf("was it red?... : %s", err.Error())
		}
	}

	// Loop over ClusterAdmissionPolicies performing actions
	for _, caPolicyObj := range globals.Application.ClusterAdmissionPolicyPool.Pool[resourcePattern] {

		specificTemplateInjectedObject := commonTemplateInjectedObject

		// Retrieve the sources declared per policy
		for sourceIndex, sourceItem := range caPolicyObj.Spec.Sources {

			unstructuredSourceObj := globals.Application.KubeRawClient.Resource(schema.GroupVersionResource{
				Group:    sourceItem.Group,
				Version:  sourceItem.Version,
				Resource: sourceItem.Resource,
			})

			sourceListOptions := v1.ListOptions{}

			if sourceItem.Namespace != "" {
				sourceListOptions.FieldSelector = fmt.Sprintf("metadata.namespace=%s", sourceItem.Namespace)
			}

			if sourceItem.Name != "" {
				if sourceListOptions.FieldSelector != "" {
					sourceListOptions.FieldSelector += ","
				}
				sourceListOptions.FieldSelector = fmt.Sprintf("metadata.name=%s", sourceItem.Name)
			}

			unstructuredSourceObjList, err := unstructuredSourceObj.List(request.Context(), sourceListOptions)
			if err != nil {
				coreLog.Print("where BLACK people...")
			}

			// Initialize ["sources"] key to store a map[int][]unstructured.Unstructured
			tmpSources, ok := specificTemplateInjectedObject["sources"].(map[int][]unstructured.Unstructured)
			if !ok {
				tmpSources = make(map[int][]unstructured.Unstructured)
			}

			tmpSources[sourceIndex] = append(tmpSources[sourceIndex], unstructuredSourceObjList.Items...)
			specificTemplateInjectedObject["sources"] = tmpSources
		}

		// Evaluate template conditions
		var conditionPassed []bool
		for _, condition := range caPolicyObj.Spec.Conditions {
			parsedKey, err := template.EvaluateTemplate(condition.Key, specificTemplateInjectedObject)
			if err != nil {
				coreLog.Printf("a dream that was not fair... %s", err.Error())
				conditionPassed = append(conditionPassed, false)
				continue
			}

			//coreLog.Print(parsedKey)
			conditionPassed = append(conditionPassed, parsedKey == condition.Value)
		}

		// When some condition is not met, evaluate message's template and emit a negative response
		if slices.Contains(conditionPassed, false) {
			coreLog.Printf("se fue al pozo...")
			continue
		}

	}

	response.WriteHeader(http.StatusOK)
}
