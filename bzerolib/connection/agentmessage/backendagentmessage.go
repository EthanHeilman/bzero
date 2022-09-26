package agentmessage

type BackendAgentMessage struct {
	MessageType MessageType `json:"messageType"`
	Timestamp   int64       `json:"timestamp"`
}

const (
	// messages sent directly to the bastionzero backend
	GetAgentIdentityToken MessageType = "getAgentIdentityToken"
	GetControlChannel     MessageType = "getControlChannel"
	OpenControlChannel    MessageType = "openControlChannel"
	OpenAgentWebsocket    MessageType = "openAgentWebsocket"
)
