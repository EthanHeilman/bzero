/*
This package defines all of the messages that are used at the AgentMessage level.
It defines the different types of messages (MessageType) and correlated payload
structs: the 4 types of keysplitting messages and agent output streams.
*/
package agentmessage

const CurrentVersion = "1.0"

type AgentMessage struct {
	ChannelId      string `json:"channelId"` // acts like a session id to tie messages to a keysplitting hash chain
	MessageType    string `json:"messageType"`
	SchemaVersion  string `json:"schemaVersion" default:"1.0"`
	MessagePayload []byte `json:"messagePayload"`
}

// The different categories of messages we might send/receive
type MessageType string

const (
	// All keysplittings messages: Syn, SynAck, Data, DataAck
	Keysplitting MessageType = "keysplitting"

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

	// regular health checks with the agent to make sure it's doing well
	HealthCheck MessageType = "healthcheck"

	// control channel message to update valid cluster users for a kube cluster
	ClusterUsers MessageType = "clusterusers"

	// poison pill message - an order from an admin to restart
	Restart MessageType = "restart"
)
