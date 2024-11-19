package xyz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/template"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

		// When some condition is not met, evaluate message's template and emit a response
		if slices.Contains(conditionPassed, false) {
			parsedMessage, err := template.EvaluateTemplate(caPolicyObj.Spec.Message.Template, &specificTemplateInjectedObject)
			if err != nil {
				logger.Info(fmt.Sprintf("failed parsing message template: %s", err.Error()))
				return
			}
			reviewResponse.Response.Result.Message = parsedMessage

			// Create the Event in Kubernetes about involved object
			err = createKubeEvent(request.Context(), "default", requestObject, caPolicyObj, parsedMessage)
			if err != nil {
				logger.Info(fmt.Sprintf("failed creating kubernetes event: %s", err.Error()))
				return
			}

			// When the policy is in Audit mode, allow it anyway
			if caPolicyObj.Spec.FailureAction == v1alpha1.FailureActionAudit {
				reviewResponse.Response.Allowed = true
				logger.Info(fmt.Sprintf("object accepted with unmet conditions: %s", parsedMessage))
			} else {
				logger.Info(fmt.Sprintf("object rejected due to unmet conditions: %s", parsedMessage))
			}
			return
		}
	}

	reviewResponse.Response.Allowed = true
	reviewResponse.Response.Result = &metav1.Status{}
}

// getKubeResourceList returns an unstructuredList of resources selected by params
func getKubeResourceList(ctx context.Context, group, version, resource, namespace, name string) (
	resourceList *unstructured.UnstructuredList, err error) {

	unstructuredSourceObj := globals.Application.KubeRawClient.Resource(schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	})

	sourceListOptions := metav1.ListOptions{}

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

// createKubeEvent TODO
func createKubeEvent(ctx context.Context, namespace string, object map[string]interface{},
	policy v1alpha1.ClusterAdmissionPolicy, message string) (err error) {

	objectData, err := GetObjectBasicData(&object)
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
		Action:              "Reviewed",
		Reason:              "ClusterAdmissionPolicyConfigured",

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
