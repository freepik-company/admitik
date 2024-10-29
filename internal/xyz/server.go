package xyz

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/log"

	coreLog "log"
)

// HttpServer represents TODO
type HttpServer struct {
	*http.Server
}

// TODO
func NewHttpServer() (server *HttpServer) {

	server = &HttpServer{
		&http.Server{},
	}

	return server
}

// TODO
func (s *HttpServer) setAddr(addr string) {
	s.Server.Addr = addr
}

// TODO
func (s *HttpServer) setHandler(handler http.Handler) {
	s.Server.Handler = handler
}

// TODO
func (s *HttpServer) handleRequest(response http.ResponseWriter, request *http.Request) {
	logger := log.FromContext(request.Context())
	_ = logger

	coreLog.Print(request)

	response.WriteHeader(http.StatusOK)

}
