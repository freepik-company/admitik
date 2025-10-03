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
	"errors"
	"fmt"
	"net/http"
	"time"

	//
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	sourcesRegistry "github.com/freepik-company/admitik/internal/registry/sources"
)

const (
	AdmissionServerValidationPath = "/validate"
	AdmissionServerMutationPath   = "/mutate"

	//
	controllerContextFinishedMessage = "Controller finished by context"
)

// AdmissionServerDependencies represents the dependencies needed by the AdmissionServer to work
type AdmissionServerDependencies struct {
	Context *context.Context

	//
	ClusterValidationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterValidationPolicy]
	ClusterMutationPolicyRegistry   *policyStore.PolicyStore[*v1alpha1.ClusterMutationPolicy]
	SourcesRegistry                 *sourcesRegistry.SourcesRegistry
}

// AdmissionServerOptions represents available options that can be passed
// to AdmissionServer on start
type AdmissionServerOptions struct {
	//
	ServerAddr string
	ServerPort int
	ServerPath string

	//
	TLSCertificate string
	TLSPrivateKey  string
}

// AdmissionServer represents the server that process coming events against
// the conditions defined in Cluster{Validation|Mutation}Policy CRs
type AdmissionServer struct {
	// Following interface is just needed to register this controller into Controller Runtime manager and let it
	// launch the controller across all the Admitik replicas or just in the elected leader.
	manager.LeaderElectionRunnable

	//
	options AdmissionServerOptions

	// Injected dependencies
	dependencies AdmissionServerDependencies
}

func NewAdmissionServer(options AdmissionServerOptions,
	dependencies AdmissionServerDependencies) *AdmissionServer {

	return &AdmissionServer{
		options:      options,
		dependencies: dependencies,
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// This is needed to inform Controller Runtime manager whether this controller needs a leader or not.
func (as *AdmissionServer) NeedLeaderElection() bool {
	return false
}

// Start prepares and runs the HTTP server.
// It gracefully drains connections on application context death.
func (as *AdmissionServer) Start(ctx context.Context) (err error) {
	logger := log.FromContext(ctx).WithValues("controller", "admissionserver")

	logger.Info("Starting Server", "address", as.options.ServerAddr, "port", as.options.ServerPort)
	customServer, err := NewHttpServer(&as.dependencies)
	if err != nil {
		return err
	}

	// Create the webserver to serve the requests
	mux := http.NewServeMux()
	mux.HandleFunc(as.options.ServerPath+AdmissionServerValidationPath, customServer.handleValidationRequest)
	mux.HandleFunc(as.options.ServerPath+AdmissionServerMutationPath, customServer.handleMutationRequest)

	// Configure and use the server previously crafted
	customServer.setAddr(fmt.Sprintf("%s:%d", as.options.ServerAddr, as.options.ServerPort))
	customServer.setHandler(mux)

	// Manage context cancellation gracefully for pending HTTP connections
	shutdownComplete := make(chan struct{})
	go func() {
		<-ctx.Done()
		logger.Info(controllerContextFinishedMessage)

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := customServer.Shutdown(shutdownCtx); err != nil {
			logger.Error(err, "Error during server shutdown")
		}
		close(shutdownComplete)
	}()

	// Finally, launch the server
	if as.options.TLSCertificate != "" && as.options.TLSPrivateKey != "" {
		err = customServer.ListenAndServeTLS(as.options.TLSCertificate, as.options.TLSPrivateKey)
	} else {
		err = customServer.ListenAndServe()
	}

	// When Shutdown is called, ListenAndServe immediately returns ErrServerClosed
	// Make sure we wait for Shutdown to complete
	if errors.Is(err, http.ErrServerClosed) {
		logger.Info("Waiting for shutdown to complete")
		<-shutdownComplete
		logger.Info("Shutdown completed")
		return nil
	}

	return err
}
