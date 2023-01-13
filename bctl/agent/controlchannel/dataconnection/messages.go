package dataconnection

import (
	"encoding/json"
	"time"

	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
)

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

// type used for custom unmarshal deserialization
type Duration struct {
	time.Duration
}

type DaemonConnectedWebsocketMessage struct {
	IdleTimeout Duration `json:"idleTimeout"`
}

// Use a custom deserializer to convert to a time.Duration from number of nano seconds
func (duration *Duration) UnmarshalJSON(data []byte) error {
	var nanoSeconds int64
	if err := json.Unmarshal(data, &nanoSeconds); err != nil {
		return err
	} else {
		duration.Duration = time.Duration(nanoSeconds)
		return nil
	}
}
