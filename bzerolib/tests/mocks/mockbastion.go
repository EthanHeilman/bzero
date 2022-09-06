package mocks

import (
	"net/http"
	"net/http/httptest"
)

type MockBastion struct {
	server *httptest.Server

	Addr      string
	Recorders map[string]*httptest.ResponseRecorder
}

func NewMockBastion(handlers ...MockHandler) *MockBastion {
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

	return &MockBastion{
		server:    s,
		Addr:      s.URL,
		Recorders: recorderMap,
	}
}

func (m *MockBastion) Close() {
	m.server.Close()
}
