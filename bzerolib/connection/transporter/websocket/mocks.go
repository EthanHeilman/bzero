package websocket

import (
	"fmt"
	"net"
	"net/http"

	"bastionzero.com/bzerolib/logger"
	"github.com/gorilla/websocket"
)

type MockWebsocketServer struct {
	logger   *logger.Logger
	listener net.Listener

	Addr          string
	ReceivedBytes chan []byte
}

// Adapted from: https://golangdocs.com/golang-gorilla-websockets
func NewMockWebsocketServer(logger *logger.Logger) *MockWebsocketServer {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		logger.Errorf("failed to setup listener")
	}

	mockServer := &MockWebsocketServer{
		logger:        logger,
		listener:      listener,
		Addr:          fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port),
		ReceivedBytes: make(chan []byte, 1),
	}

	go func() {
		http.Serve(mockServer.listener, mockServer)
	}()

	return mockServer
}

func (m *MockWebsocketServer) Shutdown() {
	m.listener.Close()
}

func (m *MockWebsocketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}

	// Upgrade our raw HTTP connection to a websocket based one
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		m.logger.Errorf("Error during connection upgradation: %s", err)
		return
	}
	defer conn.Close()

	// The event loop
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			m.logger.Errorf("Error during message reading: %s", err)
			break
		}

		m.ReceivedBytes <- message

		err = conn.WriteMessage(messageType, message)
		if err != nil {
			m.logger.Errorf("Error during message writing: %s", err)
			break
		}
	}
}
