package server

import (
	"net/http"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/gorilla/websocket"
)

type WebsocketServer struct {
	logger *logger.Logger
	conn   *websocket.Conn
}

func NewWebsocketServer(logger *logger.Logger) *WebsocketServer {
	return &WebsocketServer{
		logger: logger,
	}
}

func (w *WebsocketServer) Serve(writer http.ResponseWriter, request *http.Request) {
	upgrader := websocket.Upgrader{}
	if conn, err := upgrader.Upgrade(writer, request, nil); err != nil {
		w.logger.Errorf("failed to upgrate websocket: %s", err)
		return
	} else {
		w.conn = conn
	}

	defer w.conn.Close()

	for {
		if messageType, message, err := w.conn.ReadMessage(); err != nil {
			w.logger.Errorf("failed to read from websocket connection: %s", err)
			return
		} else if err := w.conn.WriteMessage(messageType, message); err != nil {
			w.logger.Errorf("failed to write to websocket connection: %s", err)
			return
		}
	}
}

func (w *WebsocketServer) ForceClose() {
	w.conn.Close()
}

func (w *WebsocketServer) Close() {
	// elegant close
	message := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	w.conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
}
