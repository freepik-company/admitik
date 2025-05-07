package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"freepik.com/admitik/api/v1alpha1"
	"freepik.com/admitik/internal/globals"
	"freepik.com/admitik/internal/template"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
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

// fetchPolicySources TODO
func (s *HttpServer) fetchPolicySources(cmPolicyObj any) (results map[int][]map[string]any) {

	results = make(map[int][]map[string]any)

	var policySources []v1alpha1.SourceT

	switch p := (cmPolicyObj).(type) {
	case *v1alpha1.ClusterAdmissionPolicy:
		policySources = p.Spec.Sources
	case *v1alpha1.ClusterMutationPolicy:
		policySources = p.Spec.Sources
	default:
		return results
	}

	for sourceIndex, sourceItem := range policySources {

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

// createKubeEvent creates a modern event in Kubernetes with data given by params
func createKubeEvent[PolicyType v1alpha1.ClusterAdmissionPolicy | v1alpha1.ClusterMutationPolicy](ctx context.Context, namespace string, object map[string]interface{},
	policy PolicyType, action, message string) (err error) {

	objectData, err := globals.GetObjectBasicData(&object)
	if err != nil {
		return err
	}

	var policyApiVersion, policyKind, policyName string

	switch p := any(policy).(type) {
	case v1alpha1.ClusterAdmissionPolicy:
		policyApiVersion = p.APIVersion
		policyKind = p.Kind
		policyName = p.Name
	case v1alpha1.ClusterMutationPolicy:
		policyApiVersion = p.APIVersion
		policyKind = p.Kind
		policyName = p.Name
	default:
		return fmt.Errorf("unsupported policy type")
	}

	eventObj := eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "admission-",
		},

		EventTime:           metav1.NewMicroTime(time.Now()),
		ReportingController: "admitik",
		ReportingInstance:   "admission-server",
		Action:              action,
		Reason:              "ClusterMutationPolicyAudit",

		Regarding: corev1.ObjectReference{
			APIVersion: objectData["apiVersion"].(string),
			Kind:       objectData["kind"].(string),
			Name:       objectData["name"].(string),
			Namespace:  objectData["namespace"].(string),
		},

		Related: &corev1.ObjectReference{
			APIVersion: policyApiVersion,
			Kind:       policyKind,
			Name:       policyName,
		},

		Note: message,
		Type: "Normal",
	}

	_, err = globals.Application.KubeRawCoreClient.EventsV1().Events(namespace).
		Create(ctx, &eventObj, metav1.CreateOptions{})

	return err
}
