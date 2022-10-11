package controlchannel

import (
	"bastionzero.com/bctl/v1/bzerolib/telemetry"
	"bastionzero.com/bctl/v1/bzerolib/telemetry/throughput"
)

type HeartbeatMessage struct {
	Alive              bool `json:"alive"`
	OpenedConnections  int  `json:"openedConnections"`
	ClosedConnections  int  `json:"closedConnections"`
	OpenedDataChannels int  `json:"openedDataChannels"`
	ClosedDataChannels int  `json:"closedDataChannels"`

	// the longer, less readable objects go at the bottom
	ProcessStats telemetry.ProcessStats `json:"processStats"`
	Throughput   ThroughputSummary      `json:"throughput"`
}

type ThroughputSummary struct {
	InboundAgentMessage  throughput.Throughput `json:"inboundAgentMessage"`
	OutboundAgentMessage throughput.Throughput `json:"outboundAgentMessage"`

	InboundBytes  throughput.Throughput `json:"inboundBytes"`
	OutboundBytes throughput.Throughput `json:"outboundBytes"`

	InboundSignalR  throughput.Throughput `json:"inboundSignalR"`
	OutboundSignalR throughput.Throughput `json:"outboundSignalR"`
}

type ClusterUsersMessage struct {
	ClusterUsers []string `json:"clusterUsers"`
}

// connection and datachannel management payloads
type OpenWebsocketMessage struct {
	ConnectionId         string `json:"connectionId"`
	ConnectionServiceUrl string `json:"connectionServiceUrl"`
}

type CloseWebsocketMessage struct {
	ConnectionId string `json:"connectionId"`
	Reason       string `json:"reason"`
}

type OpenDataChannelMessage struct {
	ConnectionId  string `json:"connectionId"`
	DataChannelId string `json:"dataChannelId"`
	Syn           []byte `json:"syn"`
}

type CloseDataChannelMessage struct {
	DataChannelId string `json:"dataChannelId"`
	ConnectionId  string `json:"connectionId"`
}

type RestartAgentMessage struct {
	RestartedBy string `json:"restartedBy"`
	RestartedAt string `json:"restartedAt"`
}
