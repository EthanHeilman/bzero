package dataconnection

// Message received when agent closes the websocket
type CloseDaemonWebsocketMessage struct {
	Reason string `json:"reason"`
}

// Message sent to agent when the daemon closes the websocket
type CloseAgentWebsocketMessage struct {
	Reason string `json:"reason"`
}
