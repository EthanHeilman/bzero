package tests

import (
	"net/http"
	"net/http/httptest"
	"path"
)

type MockServer struct {
	server *httptest.Server

	Url string
}

type MockHandler struct {
	Endpoint    string
	HandlerFunc http.HandlerFunc
}

func NewMockServer(handlers ...MockHandler) *MockServer {
	mux := http.NewServeMux()

	for _, handler := range handlers {
		fullEndpoint := path.Join("/", handler.Endpoint)
		mux.HandleFunc(fullEndpoint, handler.HandlerFunc)
	}

	s := httptest.NewServer(mux)

	return &MockServer{
		server: s,
		Url:    s.URL,
	}
}

func (m *MockServer) Close() {
	m.server.Close()
}
