package mocks

import (
	"net/http"
	"net/http/httptest"
)

type MockServer struct {
	server *httptest.Server

	Addr      string
	Recorders map[string]*httptest.ResponseRecorder
}

type MockHandler struct {
	Endpoint    string
	HandlerFunc http.HandlerFunc
}

func NewMockServer(handlers ...MockHandler) *MockServer {
	mux := http.NewServeMux()

	recorderMap := make(map[string]*httptest.ResponseRecorder)
	for _, handler := range handlers {
		w := httptest.NewRecorder()
		wrapperFunc := func(writer http.ResponseWriter, r *http.Request) {
			handler.HandlerFunc(w, r)
		}
		mux.HandleFunc(handler.Endpoint, wrapperFunc)

		recorderMap[handler.Endpoint] = w
	}

	s := httptest.NewServer(mux)

	return &MockServer{
		server:    s,
		Addr:      s.URL,
		Recorders: recorderMap,
	}
}

func (m *MockServer) Close() {
	m.server.Close()
}
