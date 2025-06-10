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
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/wI2L/jsondiff"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	//
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/common"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/template"
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
	// in later template evaluation stage
	commonTemplateInjectedObject := template.InjectedDataT{}
	commonTemplateInjectedObject.Initialize()

	//commonTemplateInjectedObject := map[string]interface{}{}
	err = s.extractAdmissionRequestData(&requestObj, &commonTemplateInjectedObject)
	if err != nil {
		logger.Info(fmt.Sprintf("failed extracting data from AdmissionReview: %s", err.Error()))
		return
	}

	// Loop over ClusterMutationPolicy objects collecting the patches to apply
	// At this point, some extra params will be added to the object that will be injected in template
	jsonPatchOperations := jsondiff.Patch{}

	cmPolicyList := s.dependencies.ClusterMutationPolicyRegistry.GetResources(resourcePattern)
	for _, cmPolicyObj := range cmPolicyList {

		// Automatically add some information to the logs
		logger = logger.WithValues("ClusterMutationPolicy", cmPolicyObj.Name)

		// Retrieve the sources declared per policy
		specificTemplateInjectedObject := commonTemplateInjectedObject
		specificTemplateInjectedObject.Sources = common.FetchPolicySources(cmPolicyObj, s.dependencies.SourcesRegistry)

		// Evaluate template conditions
		conditionsPassed, condErr := common.IsPassingConditions(cmPolicyObj.Spec.Conditions, &specificTemplateInjectedObject)
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

		parsedPatch, err = template.EvaluateTemplate(cmPolicyObj.Spec.Patch.Engine, cmPolicyObj.Spec.Patch.Template, &specificTemplateInjectedObject)
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
		err = common.CreateKubeEvent(request.Context(), "default", "admission-server",
			commonTemplateInjectedObject.Object, *cmPolicyObj, kubeEventAction, kubeEventMessage)
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
func generateJsonPatchOperations(objectToPatch []byte, patchType string, patch []byte) (jsonPatchOperations jsondiff.Patch, err error) {

	var patchedObjectBytes []byte
	patchType = strings.ToLower(patchType)

	// Apply user-defined patch to the entering object
	switch patchType {
	case v1alpha1.MutationPatchTypeMerge:
		var tmpPatchBytes []byte
		tmpPatchBytes, err = yaml.YAMLToJSON(patch)
		if err != nil {
			return nil, err
		}

		patchedObjectBytes, err = jsonpatch.MergePatch(objectToPatch, tmpPatchBytes)
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

// dryRunPatchedObject use a dynamic client for validating the patched object through dry-run
// TODO: Implement this DRY-RUN as a test before sending the patch to Kubernetes
func dryRunPatchedObject(req *admissionv1.AdmissionRequest, patched []byte) error {
	var err error

	// Ignore operations different from CREATE or UPDATE
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return nil
	}

	// Transform patched object
	var obj unstructured.Unstructured
	if err := json.Unmarshal(patched, &obj.Object); err != nil {
		return fmt.Errorf("failed to unmarshal patched object: %w", err)
	}

	// Rebuild GVR for the resource
	gvr := schema.GroupVersionResource{
		Group:    req.Resource.Group,
		Version:  req.Resource.Version,
		Resource: req.Resource.Resource,
	}
	namespace := req.Namespace

	//
	resourceClient := globals.Application.KubeRawClient.Resource(gvr)
	var resource dynamic.ResourceInterface = resourceClient
	if namespace != "" {
		resource = resourceClient.Namespace(namespace)
	}

	switch req.Operation {
	case admissionv1.Create:
		_, err = resource.Create(globals.Application.Context, &obj, metav1.CreateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
	case admissionv1.Update:
		_, err = resource.Update(globals.Application.Context, &obj, metav1.UpdateOptions{
			DryRun: []string{metav1.DryRunAll},
		})
	}

	if err != nil {
		return fmt.Errorf("dry-run failed: %w", err)
	}

	return nil
}
