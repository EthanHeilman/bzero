package registration

import "bastionzero.com/bctl/v1/bctl/agent/agenttype"

// Register logic
type ActivationTokenRequest struct {
	TargetName string              `json:"targetName"`
	AgentType  agenttype.AgentType `json:"agentType"`
}

type ActivationTokenResponse struct {
	ActivationToken   string `json:"activationToken"`
	ExistingClusterId string `json:"existingClusterId"`
}

type RegistrationRequest struct {
	PublicKey       string `json:"publicKey"`
	ActivationCode  string `json:"activationCode"`
	Version         string `json:"version"`
	EnvironmentId   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	TargetName      string `json:"targetName"`
	TargetHostName  string `json:"targetHostName"`
	TargetType      string `json:"agentType"`
	TargetId        string `json:"targetId"`
	Region          string `json:"region"`
}

type RegistrationResponse struct {
	TargetName      string   `json:"targetName"`
	OrgID           string   `json:"externalOrganizationId"`
	OrgProvider     string   `json:"externalOrganizationProvider"`
	JwksUrlPatterns []string `json:"allowedJwksUrlPatterns"`
}

type GetConnectionServiceResponse struct {
	ConnectionServiceUrl string `json:"connectionServiceUrl"`
}
