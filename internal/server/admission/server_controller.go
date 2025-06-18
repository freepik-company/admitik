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
	"fmt"
	"net/http"
	"time"

	//
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	clusterMutationpolicyRegistry "freepik.com/admitik/internal/registry/clustermutationpolicy"
	clusterValidationpolicyRegistry "freepik.com/admitik/internal/registry/clustervalidationpolicy"
	sourcesRegistry "freepik.com/admitik/internal/registry/sources"
)

const (
	AdmissionServerValidationPath = "/validate"
	AdmissionServerMutationPath   = "/mutate"

	//
	controllerContextFinishedMessage = "admission.AdmissionController finished by context"
)

// AdmissionServerDependencies represents the dependencies needed by the AdmissionServer to work
type AdmissionServerDependencies struct {
	ClusterValidationPolicyRegistry *clusterValidationpolicyRegistry.ClusterValidationPolicyRegistry
	ClusterMutationPolicyRegistry   *clusterMutationpolicyRegistry.ClusterMutationPolicyRegistry
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
	//
	options AdmissionServerOptions

	// Injected dependencies
	dependencies AdmissionServerDependencies
}

// TODO
func NewAdmissionServer(options AdmissionServerOptions,
	dependencies AdmissionServerDependencies) *AdmissionServer {

	return &AdmissionServer{
		options:      options,
		dependencies: dependencies,
	}
}

// Start launches the AdmissionServer and keeps it alive
// It kills the server on application context death, and rerun the process when failed
func (as *AdmissionServer) Start(ctx context.Context) {
	logger := log.FromContext(ctx)

	for {
		select {
		case <-ctx.Done():
			logger.Info(controllerContextFinishedMessage)
			return
		default:
			logger.Info(fmt.Sprintf("Starting HTTP server: %s:%d", as.options.ServerAddr, as.options.ServerPort))
			err := as.runWebserver()
			logger.Info(fmt.Sprintf("HTTP server failed: %s", err.Error()))
		}

		time.Sleep(2 * time.Second)
	}
}

// runWebserver prepares and runs the HTTP server
func (as *AdmissionServer) runWebserver() (err error) {

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

	if as.options.TLSCertificate != "" && as.options.TLSPrivateKey != "" {
		err = customServer.ListenAndServeTLS(as.options.TLSCertificate, as.options.TLSPrivateKey)
	} else {
		err = customServer.ListenAndServe()
	}

	return err
}
