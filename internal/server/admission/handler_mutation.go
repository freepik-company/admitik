package admission

import (
	"encoding/json"
	"fmt"
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/template"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/wI2L/jsondiff"
	"io"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// handleRequest handles the incoming requests
func (s *HttpServer) handleMutationRequest(response http.ResponseWriter, request *http.Request) {
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

	logger.Info("object under mutation review")

	// Assume that the request is allowed by default
	// Done in purpose as validation webhooks are executed later
	reviewResponse := &requestObj
	reviewResponse.Response = &admissionv1.AdmissionResponse{
		UID:     requestObj.Request.UID,
		Allowed: true,
		Result:  &metav1.Status{},
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

	// Craft the resourcePattern to look for the ClusterMutationPolicy objects in the pool
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

	requestObject, ok := commonTemplateInjectedObject["object"].(map[string]interface{})
	if !ok {
		logger.Info("failed converting types for presented resource. Kubernetes Event creation skipped")
	}

	// Loop over ClusterMutationPolicy objects collecting the patches to apply
	// At this point, some extra params will be added to the object that will be injected in template
	jsonPatchOperations := jsondiff.Patch{}

	cmPolicyList := s.dependencies.ClusterMutationPoliciesRegistry.GetResources(resourcePattern)
	for _, cmPolicyObj := range cmPolicyList {

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterMutationPolicy", cmPolicyObj.Name)

		// Retrieve the sources declared per policy
		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject["sources"] = s.fetchPolicySources(cmPolicyObj)

		// Evaluate template conditions
		conditionsPassed, condErr := s.isPassingConditions(cmPolicyObj.Spec.Conditions, &specificTemplateInjectedObject)
		if condErr != nil {
			logger.Info(fmt.Sprintf("failed evaluating conditions: %s", condErr.Error()))
		}

		// Conditions are not met, skip patching the resource
		if !conditionsPassed {
			continue
		}

		// When some condition is not met, evaluate patch's template and emit a response
		var kubeEventAction string = "MutationAborted"
		var kubeEventMessage string

		var parsedPatch string
		var tmpJsonPatchOperations jsondiff.Patch

		switch cmPolicyObj.Spec.Patch.Engine {
		case v1alpha1.TemplateEngineStarlark:
			parsedPatch, err = template.EvaluateTemplateStarlark(cmPolicyObj.Spec.Patch.Template, &specificTemplateInjectedObject)
		default:
			parsedPatch, err = template.EvaluateTemplate(cmPolicyObj.Spec.Patch.Template, &specificTemplateInjectedObject)
		}

		if err != nil {
			logger.Info(fmt.Sprintf("failed parsing patch template: %s", err.Error()))
			kubeEventMessage = "Patch template failed. More info in controller logs."
			goto createKubeEvent
		}

		tmpJsonPatchOperations, err = generateJsonPatchOperations(requestObj.Request.Object.Raw, cmPolicyObj.Spec.Patch.Type, []byte(parsedPatch))
		if err != nil {
			logger.Info(fmt.Sprintf("failed generating canonical jsonPatch operations for Kube API server: %s", err.Error()))
			kubeEventMessage = "Generated patch is invalid. More info in controller logs."
			goto createKubeEvent
		}

		jsonPatchOperations = append(jsonPatchOperations, tmpJsonPatchOperations...)
		continue

	createKubeEvent:
		err = createKubeEvent(request.Context(), "default", requestObject, *cmPolicyObj, kubeEventAction, kubeEventMessage)
		if err != nil {
			logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
		}
	}

	// All working mutation patches are collected from policies, send them to Kubernetes
	jsonPatchOperationBytes, err := json.Marshal(jsonPatchOperations)

	reviewResponse.Response.Patch = jsonPatchOperationBytes
	patchType := admissionv1.PatchTypeJSONPatch
	reviewResponse.Response.PatchType = &patchType
}

// generateJsonPatchOperations return a group of JsonPatch operations to mutate an object from its original
// state to a final state. It's compatible with 'jsonpatch' and 'strategicmerge' patch types.
func generateJsonPatchOperations(objectToPatch []byte, patchType string, patch []byte) (results jsondiff.Patch, err error) {

	var patchedObjectBytes []byte

	// Apply user-defined patch to the entering object
	switch patchType {
	case v1alpha1.MutationPatchTypeMerge:
		patchedObjectBytes, err = jsonpatch.MergePatch(objectToPatch, patch)
		if err != nil {
			return nil, err
		}
	default:
		var tmpPatch jsonpatch.Patch
		tmpPatch, err = jsonpatch.DecodePatch(patch)
		if err != nil {
			return nil, err
		}

		patchedObjectBytes, err = tmpPatch.Apply(objectToPatch)
		if err != nil {
			return nil, err
		}
	}

	// Calculate the difference for Kubernetes API
	// Store only successful operations in the operations list
	var tmpJsonPatchOperations jsondiff.Patch

	tmpJsonPatchOperations, err = jsondiff.CompareJSON(objectToPatch, patchedObjectBytes)
	if err != nil {
		return nil, err
	}

	return tmpJsonPatchOperations, nil
}
