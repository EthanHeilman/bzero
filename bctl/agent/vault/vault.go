package vault

import (
	"bastionzero.com/bctl/v1/bzerolib/keypair"
)

type vault struct {
	Version string

	// Who is in charge of this agent? Kubernetes or SystemD
	AgentType string

	// Agent signature key pair
	// Our public key is stored as a base64 encoded string because
	// it is only ever used to send in that format
	// The private key is used to sign and therefore is stored in its
	// most usable format
	PublicKey  keypair.PublicKey
	PrivateKey keypair.PrivateKey

	// OIDC-compliant token used for authenticating to BastionZero
	AgentIdentityToken string

	// This is the primary key of the agent table, we use this because
	// we currently have no way to guarantee unique public keys
	TargetId string

	// These values are compared against the user's id token to verify
	// they belong to the same org as the agent
	IdpProvider string
	IdpOrgId    string

	// URL of our Bastion
	ServiceUrl string

	// For reporting back to BastionZero why the agent shutdown
	ShutdownReason string
	ShutdownState  map[string]string
}
