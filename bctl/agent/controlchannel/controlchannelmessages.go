package controlchannel

import "encoding/json"

type HeartbeatMessage struct {
	Alive           bool            `json:"alive"`
	NumDataChannels uint32          `json:"numDataChannels"`
	ProcessStats    json.RawMessage `json:"processStats"`
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
