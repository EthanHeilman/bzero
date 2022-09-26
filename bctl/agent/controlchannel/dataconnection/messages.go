package dataconnection

import am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"

type OpenAgentWebsocketMessage struct {
	am.BackendAgentMessage
	ConnectionId   string
	ConnectionType string
}

type OpenDataChannelMessage struct {
	DataChannelId string `json:"dataChannelId"`
	Syn           []byte `json:"syn"`
}

type CloseDataChannelMessage struct {
	DataChannelId string `json:"dataChannelId"`
}

// Message sent to daemon when agent closes the websocket
type CloseDaemonWebsocketMessage struct {
	Reason string `json:"reason"`
}

// Message received when daemon closes the websocket
type CloseAgentWebsocketMessage struct {
	Reason string `json:"reason"`
}
