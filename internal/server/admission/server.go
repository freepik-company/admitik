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
	"net/http"
)

// HttpServer represents a simple HTTP server
type HttpServer struct {
	*http.Server

	// Injected dependencies
	dependencies *AdmissionServerDependencies
}

// NewHttpServer creates a new HttpServer
func NewHttpServer(dependencies *AdmissionServerDependencies) *HttpServer {
	return &HttpServer{
		&http.Server{},
		dependencies,
	}
}

// setAddr sets the address for the server
func (s *HttpServer) setAddr(addr string) {
	s.Server.Addr = addr
}

// setHandler sets the handler for the server
func (s *HttpServer) setHandler(handler http.Handler) {
	s.Server.Handler = handler
}
