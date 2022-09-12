package tests

import (
	"net/http"
	"net/http/httptest"
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
		mux.HandleFunc(handler.Endpoint, handler.HandlerFunc)
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
