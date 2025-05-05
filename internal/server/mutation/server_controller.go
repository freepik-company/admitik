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

package mutation

import (
	"context"
	"fmt"
	"net/http"
	"time"

	//
	"sigs.k8s.io/controller-runtime/pkg/log"

	clustermutationpoliciesRegistry "freepik.com/admitik/internal/registry/clustermutationpolicies"
	sourcesRegistry "freepik.com/admitik/internal/registry/sources"
)

const (

	//
	controllerContextFinishedMessage = "mutation.MutationController finished by context"
)

// MutationServerDependencies represents the dependencies needed by the MutationServer to work
type MutationServerDependencies struct {
	SourcesRegistry                 *sourcesRegistry.SourcesRegistry
	ClusterMutationPoliciesRegistry *clustermutationpoliciesRegistry.ClusterMutationPoliciesRegistry
}

// MutationServerOptions represents available options that can be passed
// to MutationServer on start
type MutationServerOptions struct {
	//
	ServerAddr string
	ServerPort int
	ServerPath string

	//
	TLSCertificate string
	TLSPrivateKey  string
}

// MutationServer represents the server that process coming events against
// the conditions defined in Cluster/MutationPolicy CRs
type MutationServer struct {
	//
	options MutationServerOptions

	// Injected dependencies
	dependencies MutationServerDependencies
}

// TODO
func NewMutationServer(options MutationServerOptions,
	dependencies MutationServerDependencies) *MutationServer {

	return &MutationServer{
		options:      options,
		dependencies: dependencies,
	}
}

// Start launches the MutationServer and keeps it alive
// It kills the server on application context death, and rerun the process when failed
func (as *MutationServer) Start(ctx context.Context) {
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
func (as *MutationServer) runWebserver() (err error) {

	customServer := NewHttpServer(&as.dependencies)

	// Create the webserver to serve the requests
	mux := http.NewServeMux()
	mux.HandleFunc(as.options.ServerPath, customServer.handleRequest)

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
