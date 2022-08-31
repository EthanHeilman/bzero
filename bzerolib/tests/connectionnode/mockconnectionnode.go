package connectionnode

import (
	"net/http"
	"net/http/httptest"
	"path"
)

type MockConnectionNode struct {
	mux    *http.ServeMux
	server *httptest.Server

	prefix string
}

// prefix is the endpoint we will always expect every endpoint to begin with
// e.g. /hub/agent
func New(endpointPrefix string) *MockConnectionNode {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)

	return &MockConnectionNode{
		mux:    mux,
		server: server,
		prefix: path.Join("/", endpointPrefix), // always make sure there's a leading "/"
	}
}

func (m *MockConnectionNode) Url() string {
	return m.server.URL
}

func (m *MockConnectionNode) AddHandler(endpoint string, handlerFunc http.HandlerFunc) {
	// append prefix
	fullEndpoint := path.Join(m.prefix, endpoint)

	m.mux.HandleFunc(fullEndpoint, handlerFunc)
}

func (m *MockConnectionNode) AddSignalRHub() {}

func (m *MockConnectionNode) Close() {
	m.server.Close()
}

// func (m *MockBastion) serveWebsocket(w http.ResponseWriter, r *http.Request) {
// 	upgrader := websocket.Upgrader{}

// 	// Upgrade our raw HTTP connection to a websocket based one
// 	conn, err := upgrader.Upgrade(w, r, nil)
// 	if err != nil {
// 		// m.logger.Errorf("Error during connection upgradation: %s", err)
// 		return
// 	}
// 	defer conn.Close()

// 	go func() {
// 		select {
// 		case <-m.interruptChan:
// 			conn.Close()
// 		case <-m.doneChan:
// 			message := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
// 			conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
// 		}
// 	}()

// 	// The event loop
// 	for {
// 		messageType, message, err := conn.ReadMessage()
// 		if err != nil {
// 			// m.logger.Errorf("Error during message reading: %s", err)
// 			return
// 		}

// 		// m.ReceivedBytes <- message

// 		err = conn.WriteMessage(messageType, message)
// 		if err != nil {
// 			// m.logger.Errorf("Error during message writing: %s", err)
// 			return
// 		}
// 	}
// 	// for {
// 	// 	messageType, message, err := conn.ReadMessage()
// 	// 	if err != nil {
// 	// 		// m.logger.Errorf("Error during message reading: %s", err)
// 	// 		break
// 	// 	}

// 	// 	// m.ReceivedBytes <- message

// 	// 	err = conn.WriteMessage(messageType, message)
// 	// 	if err != nil {
// 	// 		// m.logger.Errorf("Error during message writing: %s", err)
// 	// 		break
// 	// 	}
// 	// }
// }
