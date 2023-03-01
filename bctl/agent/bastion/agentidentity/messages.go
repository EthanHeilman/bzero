package agentidentity

import am "bastionzero.com/bzerolib/connection/agentmessage"

type GetAgentIdentityTokenRequest struct {
	am.BackendAgentMessage
}

type GetAgentIdentityTokenResponse struct {
	Token string `json:"token"`
}
