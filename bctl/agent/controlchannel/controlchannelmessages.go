package controlchannel

import (
	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	bzcrt "bastionzero.com/bctl/v1/bzerolib/mrtap/bzcert"
)

type HeartbeatMessage struct {
	Alive           bool   `json:"alive"`
	NumDataChannels uint32 `json:"numDataChannels"`
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

type RetrieveAgentLogsMessage struct {
	UserEmail           string `json:"userEmail"`
	UploadLogsRequestId string `json:"uploadLogsRequestId"`
}

type KeyShardMessage struct {
	TargetIds []string      `json:"virtualTargetId"`
	KeyShard  data.KeyEntry `json:"keyShard"`
}

type ServiceAccountConfiguration struct {
	JwksUrlPattern string `json:"jwksUrlPattern"`
}

type ConfigureServiceAccountMessage struct {
	ServiceAccountConfiguration ServiceAccountConfiguration `json:"serviceAccountConfiguration"`
	BZCert                      bzcrt.BZCert                `json:"bZCert"`
	Signature                   string                      `json:"signature"`
}
