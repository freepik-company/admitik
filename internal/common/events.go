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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/globals"
)

// PolicyIdentifiable is the minimal interface a policy must implement to be
// referenced in a Kubernetes event. All four policy CRD types satisfy it.
type PolicyIdentifiable interface {
	GetName() string
	GetPolicyKind() string
}

// PolicyEvent describes a single event to be emitted to the Kubernetes API.
type PolicyEvent struct {
	// Action is a short CamelCase token describing what happened
	// (e.g. "GenerationSucceeded", "GenerationAborted", "Rejected").
	Action string

	// Message is a human-readable note with the full detail — errors, reasons, etc.
	// This is what the user sees in `kubectl get events`.
	Message string

	// TargetRef optionally identifies the object that was created, updated or deleted
	// as a result of the policy action. Nil when the target is unknown (e.g. template
	// rendering failed before resolving the target).
	TargetRef *corev1.ObjectReference
}

// warningActions lists the Action suffixes that indicate a failure or warning event.
var warningActions = []string{
	"Aborted",
	"Failed",
	"Rejected",
}

// EventEmitter creates Kubernetes events linked to a trigger object and a policy.
// It encapsulates context, reporter name and logger so callers can emit events
// with a single method call.
type EventEmitter struct {
	ctx      context.Context
	reporter string
	logger   logr.Logger
}

// NewEventEmitter creates an EventEmitter bound to the given context, reporter name
// and logger. The reporter identifies the component emitting the event
// (e.g. "admission-server", "resources-controller").
func NewEventEmitter(ctx context.Context, reporter string, logger logr.Logger) *EventEmitter {
	return &EventEmitter{ctx: ctx, reporter: reporter, logger: logger}
}

// Emit creates a Kubernetes Event that links the trigger object to the given policy.
// The event type (Normal/Warning) is derived automatically from the action name.
// Any error during event creation is logged silently — callers never need to handle it.
func (e *EventEmitter) Emit(triggerObj map[string]interface{}, policy PolicyIdentifiable, pe PolicyEvent) {
	objectData, err := globals.GetObjectBasicData(&triggerObj)
	if err != nil {
		e.logger.V(1).Info("failed extracting trigger object data for event", "error", err.Error())
		return
	}

	kind := policy.GetPolicyKind()

	eventNamespace := objectData.Namespace
	if eventNamespace == "" {
		eventNamespace = "default"
	}

	apiVersion := objectData.Version
	if objectData.Group != "" {
		apiVersion = objectData.Group + "/" + objectData.Version
	}

	event := eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: e.reporter + "-",
			Namespace:    eventNamespace,
		},
		EventTime:           metav1.NewMicroTime(time.Now()),
		ReportingController: "admitik",
		ReportingInstance:   e.reporter,
		Action:              pe.Action,
		Reason:              kind + "Audit",

		Regarding: corev1.ObjectReference{
			APIVersion: apiVersion,
			Kind:       objectData.Kind,
			Name:       objectData.Name,
			Namespace:  objectData.Namespace,
		},

		Related: &corev1.ObjectReference{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       kind,
			Name:       policy.GetName(),
		},

		Note: buildNote(pe),
		Type: eventType(pe.Action),
	}

	if _, err = globals.Application.KubeRawCoreClient.EventsV1().Events(eventNamespace).
		Create(e.ctx, &event, metav1.CreateOptions{}); err != nil {
		e.logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
	}
}

// eventType returns "Warning" for failure/abort/rejection actions, "Normal" otherwise.
func eventType(action string) string {
	for _, suffix := range warningActions {
		if strings.HasSuffix(action, suffix) {
			return "Warning"
		}
	}
	return "Normal"
}

// buildNote assembles the event note from the message and optional target reference.
func buildNote(pe PolicyEvent) string {
	if pe.TargetRef == nil {
		return pe.Message
	}

	target := formatObjectRef(pe.TargetRef)
	return fmt.Sprintf("[target: %s] %s", target, pe.Message)
}

// formatObjectRef returns a compact "kind namespace/name" or "kind name" string.
func formatObjectRef(ref *corev1.ObjectReference) string {
	if ref.Namespace != "" {
		return fmt.Sprintf("%s %s/%s", ref.Kind, ref.Namespace, ref.Name)
	}
	return fmt.Sprintf("%s %s", ref.Kind, ref.Name)
}

// TargetRefFromBasicData builds a corev1.ObjectReference from ObjectBasicData.
// Convenience helper so callers don't have to assemble it manually.
func TargetRefFromBasicData(bd globals.ObjectBasicData) *corev1.ObjectReference {
	apiVersion := bd.Version
	if bd.Group != "" {
		apiVersion = bd.Group + "/" + bd.Version
	}
	return &corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       bd.Kind,
		Name:       bd.Name,
		Namespace:  bd.Namespace,
	}
}
