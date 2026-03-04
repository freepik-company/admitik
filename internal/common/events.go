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
// action describes what happened (e.g. "GenerationAborted", "Rejected").
// message provides human-readable detail for the event note.
// Any error during event creation is logged silently — callers never need to handle it.
func (e *EventEmitter) Emit(triggerObj map[string]interface{}, policy PolicyIdentifiable, action, message string) {
	objectData, err := globals.GetObjectBasicData(&triggerObj)
	if err != nil {
		e.logger.V(1).Info("failed extracting trigger object data for event", "error", err.Error())
		return
	}

	kind := policy.GetPolicyKind()

	event := eventsv1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: e.reporter + "-",
		},
		EventTime:           metav1.NewMicroTime(time.Now()),
		ReportingController: "admitik",
		ReportingInstance:   e.reporter,
		Action:              action,
		Reason:              kind + "Audit",

		Regarding: corev1.ObjectReference{
			APIVersion: strings.Join([]string{objectData.Group, objectData.Version}, "/"),
			Kind:       objectData.Kind,
			Name:       objectData.Name,
			Namespace:  objectData.Namespace,
		},

		Related: &corev1.ObjectReference{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       kind,
			Name:       policy.GetName(),
		},

		Note: message,
		Type: "Normal",
	}

	if _, err = globals.Application.KubeRawCoreClient.EventsV1().Events("default").
		Create(e.ctx, &event, metav1.CreateOptions{}); err != nil {
		e.logger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", err.Error()))
	}
}
