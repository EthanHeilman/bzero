package signalr

import "fmt"

type WebsocketNormalClosure struct {
	ServerError string
}

func (w *WebsocketNormalClosure) Error() string {
	return fmt.Sprintf("websocket was closed by the server with error: %s", w.ServerError)
}

func (w *WebsocketNormalClosure) Unwrap() error { return nil }
