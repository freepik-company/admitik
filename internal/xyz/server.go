package xyz

import (
	coreLog "log"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// HttpServer represents a simple HTTP server
type HttpServer struct {
	*http.Server
}

// NewHttpServer creates a new HttpServer
func NewHttpServer() *HttpServer {
	return &HttpServer{
		&http.Server{},
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

// handleRequest handles the incoming requests
func (s *HttpServer) handleRequest(response http.ResponseWriter, request *http.Request) {
	logger := log.FromContext(request.Context())
	_ = logger

	coreLog.Print(request)

	pepe := admissionv1.AdmissionRequest{}

	_ = pepe

	response.WriteHeader(http.StatusOK)
}
