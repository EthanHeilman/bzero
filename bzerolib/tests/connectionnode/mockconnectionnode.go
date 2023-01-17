package connectionnode

import (
	"bytes"
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

	interruptChan chan struct{}

	// public attributes
	Url      string
	Received chan *signalr.SignalRMessage
}

// Endpoint is the address at which we will serve our websocket
func New(logger *logger.Logger, endpoint string) *MockConnectionNode {
	endpoint = path.Join("/", endpoint) // always make sure there's a leading "/"

	// Create our servers
	mux := http.NewServeMux()
	httpServer := httptest.NewServer(mux)
	websocketServer := server.NewWebsocketServer(logger)

	// Serve a websocket at the root
	mux.HandleFunc(endpoint, websocketServer.Serve)

	cn := MockConnectionNode{
		logger:          logger,
		mux:             mux,
		server:          httpServer,
		websocketServer: websocketServer,
		interruptChan:   make(chan struct{}),
		Url:             httpServer.URL,
		Received:        make(chan *signalr.SignalRMessage, 50),
	}

	// Set up our go routine to turn the bytes the websocket receives into
	go func() {
		for {
			select {
			case <-cn.interruptChan:
				return
			case raw := <-cn.websocketServer.Received:
				// We may have received multiple messages in one
				splitMessages := bytes.Split(raw, []byte{signalr.TerminatorByte})

				for _, rawMessage := range splitMessages {
					// Ignore empty slices AND empty json "{}"
					if len(rawMessage) <= 2 {
						continue
					}

					var message signalr.SignalRMessage
					if err := json.Unmarshal(rawMessage, &message); err != nil {
						logger.Errorf("error unmarshalling SignalR message: %s. Error: %s", string(rawMessage), err)
					}

					var msgType = signalr.SignalRMessageType(message.Type)
					logger.Debugf("Received signalR message of type: %s", msgType.String())
					switch msgType {
					case signalr.Invocation:
						// send back a dummy invocation completion message for
						// all server invocations so that these invocations can
						// be matched in the agent's invocator. Otherwise this
						// prevents the agent's signalR client from closing.
						invocationCompletionMessage := &signalr.CompletionMessage{
							Type:         int(signalr.Completion),
							InvocationId: &message.InvocationId,
							Error:        nil,
							Result:       nil,
						}

						messageBytes, err := json.Marshal(invocationCompletionMessage)
						if err != nil {
							logger.Errorf("error marshalling invocation completion message: %+v", invocationCompletionMessage)
							return
						}
						cn.SendSignalRMessage(messageBytes)
					}

					// Push message to queue for processing
					cn.Received <- &message
				}
			}
		}
	}()

	return &cn
}

func (m *MockConnectionNode) AddEndpoint(endpoint string, handlerFunc http.HandlerFunc) {
	fullEndpoint := path.Join("/", endpoint)
	m.mux.HandleFunc(fullEndpoint, handlerFunc)
}

func (m *MockConnectionNode) SendSignalRInvocationMessage(target string, message interface{}) {
	messageBytes, _ := json.Marshal(message)

	signalRMessage := &signalr.SignalRMessage{
		Type:         int(signalr.Invocation),
		Target:       target,
		Arguments:    []json.RawMessage{messageBytes},
		InvocationId: fmt.Sprint(rand.Intn(1000)),
	}

	messageBytes, err := json.Marshal(signalRMessage)
	if err != nil {
		m.logger.Errorf("error marshalling outgoing SignalR Message: %+v", message)
		return
	}

	m.SendSignalRMessage(messageBytes)
}

func (m *MockConnectionNode) SendSignalRMessage(rawMessage []byte) {
	terminatedMessageBytes := append(rawMessage, signalr.TerminatorByte)
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
