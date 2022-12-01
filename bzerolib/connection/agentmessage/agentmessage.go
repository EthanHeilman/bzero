/*
This package defines all of the messages that are used at the AgentMessage level.
It defines the different types of messages (MessageType) and correlated payload
structs: the 4 types of MrTAP messages and agent output streams.
*/
package agentmessage

import (
	"encoding/json"
)

const CurrentVersion = "1.0"

type AgentMessage struct {
	ChannelId      string      `json:"channelId"` // acts like a session id to tie messages to a MrTAP hash chain
	MessageType    MessageType `json:"messageType"`
	SchemaVersion  string      `json:"schemaVersion" default:"1.0"`
	MessagePayload []byte      `json:"messagePayload"`
}

// The different categories of messages we might send/receive
type MessageType string

const (
	// All MrTAP messages: Syn, SynAck, Data, DataAck
	Mrtap       MessageType = "mrtap"
	MrtapLegacy MessageType = "keysplitting"

	// Agent output stream message types
	Stream MessageType = "stream"

	// Error message type for reporting all error messages
	Error MessageType = "error"

	// datachannel controller messages
	OpenDataChannel      MessageType = "openDataChannel"
	CloseDataChannel     MessageType = "closeDataChannel"
	CloseDaemonWebsocket MessageType = "closeDaemonWebsocket"
	CloseAgentWebsocket  MessageType = "closeAgentWebsocket"

	// connection controller messages
	OpenWebsocket  MessageType = "openWebsocket"
	CloseWebsocket MessageType = "closeWebsocket"

	// message for force closing all connections an agent has
	CloseAllConnections MessageType = "closeAllConnections"

	// regular health checks between the agent and the conneciton node
	HealthCheck MessageType = "healthcheck"

	// control channel message to update valid cluster users for a kube cluster
	ClusterUsers MessageType = "clusterusers"

	// poison pill message - an order from an admin to restart
	Restart MessageType = "restart"

	// not as poisonous message - an order from an admin to add a service account jwksUrlPattern in this agent
	Configure MessageType = "configure"

	// message to trigger agent to send logs to bastion
	RetrieveLogs MessageType = "retrievelogs"

	// control channel message to distribute key shards for passwordless connections
	KeyShard MessageType = "keyshard"
)

// TODO: CWC-2183; remove this logic in the far future
func (mt *MessageType) UnmarshalJSON(data []byte) error {
	var t string
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}

	if MessageType(t) == MrtapLegacy {
		*mt = Mrtap
	} else {
		*mt = MessageType(t)
	}

	return nil
}
