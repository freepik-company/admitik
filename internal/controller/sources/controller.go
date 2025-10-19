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

package sources

import (
	"context"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/informer"
	"slices"
	"strings"
	"time"

	//
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/globals"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	sourcesRegistry "github.com/freepik-company/admitik/internal/registry/sources"
)

const (
	// secondsToCheckInformerAck is the number of seconds before checking
	// whether an informer is started or not during informers' reconciling process
	secondsToCheckInformerAck = 10 * time.Second

	// secondsToReconcileInformersAgain is the number of seconds to wait
	// between the moment of launching informers, and repeating this process
	// (avoid the spam, mate)
	secondsToReconcileInformersAgain = 2 * time.Second

	//
	controllerName = "sources"

	//
	controllerContextFinishedMessage = "Controller finished by context"
)

// SourcesControllerOptions represents available options that can be passed to SourcesController on start
type SourcesControllerOptions struct {
	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration
}

type SourcesControllerDependencies struct {
	Context *context.Context

	//
	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	ClusterMutationPolicyRegistry   *policyStore.PolicyStore[*v1alpha1.ClusterMutationPolicy]
	ClusterValidationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterValidationPolicy]

	SourcesRegistry *sourcesRegistry.SourcesRegistry
}

// SourcesController represents a controller that triggers parallel threads.
// These threads watch resources defined in 'sources' section of several object types stored in registries.
// Each thread is an informer in charge of a group of resources GVRNN (Group + Version + Resource + Namespace + Name)
type SourcesController struct {
	// Following interface is just needed to register this controller into Controller Runtime manager and let it
	// launch the controller across all the Admitik replicas or just in the elected leader.
	manager.LeaderElectionRunnable

	Options      SourcesControllerOptions
	Dependencies SourcesControllerDependencies
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// This is needed to inform Controller Runtime manager whether this controller needs a leader or not.
func (r *SourcesController) NeedLeaderElection() bool {
	return false
}

// getSourcesFromPolicies returns a list of sources with all the types registered in suitable registries
func (r *SourcesController) getSourcesFromPolicies() []string {

	var referentCandidates []string

	candidatesFromGeneration := r.Dependencies.ClusterGenerationPolicyRegistry.GetReferencedSources()
	candidatesFromMutation := r.Dependencies.ClusterMutationPolicyRegistry.GetReferencedSources()
	candidatesFromValidation := r.Dependencies.ClusterValidationPolicyRegistry.GetReferencedSources()
	referentCandidates = slices.Concat(candidatesFromGeneration, candidatesFromMutation, candidatesFromValidation)

	// Filter duplicated items
	slices.Sort(referentCandidates)
	referentCandidates = slices.Compact(referentCandidates)

	return referentCandidates
}

// Start launches the SourcesController and keeps it alive
// It kills the controller on application's context death, and rerun the process when failed
func (r *SourcesController) Start(ctx context.Context) error {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)
	logger.Info("Starting Controller")

	// Keep your controller alive
	for {
		select {
		case <-(*r.Dependencies.Context).Done():
			logger.Info(controllerContextFinishedMessage)
			return nil
		default:
			r.reconcileInformers()
			time.Sleep(secondsToReconcileInformersAgain)
		}
	}
}

// reconcileInformers checks each registered extra-resource type and triggers informers
// for those that are not already started.
func (r *SourcesController) reconcileInformers() {
	r.launchDesiredInformers()
	r.cleanNotNeededInformers()
}

// launchDesiredInformers checks each registered source type and triggers informers
// for those that are not already started.
func (r *SourcesController) launchDesiredInformers() {
	sourcesCandidates := r.getSourcesFromPolicies()

	for _, resourceType := range sourcesCandidates {

		resourceTypeParts := strings.Split(resourceType, "/")

		gvr := schema.GroupVersionResource{
			Group:    resourceTypeParts[0],
			Version:  resourceTypeParts[1],
			Resource: resourceTypeParts[2],
		}

		tmpInformer := &informer.Informer{}

		isRegistered := r.Dependencies.SourcesRegistry.InformerIsRegistered(gvr)
		if !isRegistered {
			// TODO: Review the error
			tmpInformer, _ = informer.NewInformer(
				informer.Options{GVR: gvr, InformerDurationToResync: r.Options.InformerDurationToResync},
				informer.Dependencies{Context: r.Dependencies.Context, Client: globals.Application.KubeRawClient})

			r.Dependencies.SourcesRegistry.RegisterInformer(gvr, tmpInformer)
		} else {
			tmpInformer = r.Dependencies.SourcesRegistry.GetInformer(gvr)
		}

		tmpInformer.Start()
	}
}

// cleanNotNeededInformers review the 'sources' section of several object types stored in registries in the background.
// It disables the informers that are not needed and delete them from sources registry
func (r *SourcesController) cleanNotNeededInformers() {
	referentCandidates := r.getSourcesFromPolicies()
	reviewedCandidates := r.Dependencies.SourcesRegistry.GetRegisteredResourceTypes()

	for _, resourceType := range reviewedCandidates {

		if !slices.Contains(referentCandidates, common.GvrString(resourceType)) {
			r.Dependencies.SourcesRegistry.DestroyInformer(resourceType)
		}
	}
}
