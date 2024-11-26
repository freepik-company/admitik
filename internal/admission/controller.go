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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	//
)

const (

	//
	controllerContextFinishedMessage = "admission.AdmissionController finished by context"
)

// AdmissionControllerOptions represents available options that can be passed
// to AdmissionController on start
type AdmissionControllerOptions struct {
	//
	ServerAddr string
	ServerPort int
	ServerPath string

	//
	TLSCertificate string
	TLSPrivateKey  string
}

// AdmissionController represents the controller that triggers parallel threads.
// These threads process coming events against the conditions defined in Notification CRs
// Each thread is a watcher in charge of a group of resources GVRNN (Group + Version + Resource + Namespace + Name)
type AdmissionController struct {
	Client client.Client

	//
	Options AdmissionControllerOptions
}

// Start launches the XYZ.AdmissionController and keeps it alive
// It kills the controller on application context death, and rerun the process when failed
func (r *AdmissionController) Start(ctx context.Context) {
	logger := log.FromContext(ctx)

	for {
		select {
		case <-ctx.Done():
			logger.Info(controllerContextFinishedMessage)
			return
		default:
			logger.Info(fmt.Sprintf("Starting HTTP server: %s:%d", r.Options.ServerAddr, r.Options.ServerPort))
			err := r.runWebserver()
			logger.Info(fmt.Sprintf("HTTP server failed: %s", err.Error()))
		}

		time.Sleep(2 * time.Second)
	}
}

// runWebserver prepares and runs the HTTP server
func (r *AdmissionController) runWebserver() (err error) {

	customServer := NewHttpServer()

	// Create the webserver to serve the requests
	mux := http.NewServeMux()
	mux.HandleFunc(r.Options.ServerPath, customServer.handleRequest)

	// Configure and use the server previously crafted
	customServer.setAddr(fmt.Sprintf("%s:%d", r.Options.ServerAddr, r.Options.ServerPort))
	customServer.setHandler(mux)

	if r.Options.TLSCertificate != "" && r.Options.TLSPrivateKey != "" {
		err = customServer.ListenAndServeTLS(r.Options.TLSCertificate, r.Options.TLSPrivateKey)
	} else {
		err = customServer.ListenAndServe()
	}

	return err
}
