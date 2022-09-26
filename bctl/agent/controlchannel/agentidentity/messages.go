package agentidentity

import am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"

type GetAgentIdentityTokenRequest struct {
	am.BackendAgentMessage
}

type GetAgentIdentityTokenResponse struct {
	Token string `json:"token"`
}
