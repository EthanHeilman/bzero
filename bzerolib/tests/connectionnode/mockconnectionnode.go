package connectionnode

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path"

	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/tests/server"
)

type MockConnectionNode struct {
	logger *logger.Logger

	// servers
	mux             *http.ServeMux
	server          *httptest.Server
	websocketServer *server.WebsocketServer

	prefix string

	// public attributes
	Url string
}

// Endpoint is the address at which we will serve our websocket
func New(logger *logger.Logger, endpoint string) *MockConnectionNode {
	prefix := path.Join("/", endpoint) // always make sure there's a leading "/"

	// Create our servers
	mux := http.NewServeMux()
	httpServer := httptest.NewServer(mux)
	websocketServer := server.NewWebsocketServer(logger)

	// Serve a websocket at the root
	mux.HandleFunc(prefix, websocketServer.Serve)

	return &MockConnectionNode{
		logger:          logger,
		mux:             mux,
		server:          httpServer,
		websocketServer: websocketServer,
		prefix:          prefix,
		Url:             httpServer.URL,
	}
}

func (m *MockConnectionNode) AddEndpoint(endpoint string, handlerFunc http.HandlerFunc) {
	m.mux.HandleFunc(endpoint, handlerFunc)
}

func (m *MockConnectionNode) SendSignalRMessage(target string, message interface{}) {
	messageBytes, _ := json.Marshal(message)

	signalRMessage := &signalr.SignalRMessage{
		Type:         int(signalr.Invocation),
		Target:       target,
		Arguments:    []json.RawMessage{messageBytes},
		InvocationId: fmt.Sprintf("%d", rand.Intn(1000)),
	}

	trackedMessageBytes, err := json.Marshal(signalRMessage)
	if err != nil {
		m.logger.Errorf("error marshalling outgoing SignalR Message: %+v", message)
		return
	}

	terminatedMessageBytes := append(trackedMessageBytes, signalr.TerminatorByte)
	m.websocketServer.Write(terminatedMessageBytes)
}

func (m *MockConnectionNode) BreakWebsocket() {
	m.websocketServer.ForceClose()
}

func (m *MockConnectionNode) CloseWebsocket() {
	m.websocketServer.Close()
}

func (m *MockConnectionNode) Close() {
	m.websocketServer.Close()
	m.server.Close()
}
