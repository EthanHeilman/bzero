package controlconnection

import am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"

type GetConnectionServiceResponse struct {
	ConnectionServiceUrl string `json:"connectionServiceUrl"`
}

type GetControlChannel struct {
	am.BackendAgentMessage
}

type GetControlChannelResponse struct {
	ConnectionUrl    string `json:"connectionUrl"`
	ControlChannelId string `json:"controlChannelId"`
}

type OpenControlChannel struct {
	am.BackendAgentMessage
	ControlChannelId string `json:"controlChannelId"`
	ConnectionUrl    string `json:"connectionUrl"`
	Version          string `json:"version"`
}
