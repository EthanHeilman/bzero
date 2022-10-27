package data

import (
	"encoding/json"
	"errors"
	"fmt"

	"bastionzero.com/bctl/v1/bzerolib/keypair"
)

// This version was introduced in https://github.com/bastionzero/bzero/pull/166
type DataV2 struct {
	// Agent Version
	Version string

	// Who is in charge of this agent? Kubernetes or Systemd
	AgentType string

	// Agent signature key pair
	PublicKey  *keypair.PublicKey
	PrivateKey *keypair.PrivateKey

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

// In order to make the new config backwards compatible, we have to have some custom
// unmarshalling logic
func (v *DataV2) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || string(data) == `""` {
		return nil
	}

	var objmap map[string]json.RawMessage
	if err := json.Unmarshal(data, &objmap); err != nil {
		return err
	}

	var t string
	if err := json.Unmarshal(objmap["Version"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal Version: %s", err)
	} else {
		v.Version = t
	}

	if err := json.Unmarshal(objmap["ServiceUrl"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal ServiceUrl: %s", err)
	} else {
		v.ServiceUrl = t
	}

	if err := json.Unmarshal(objmap["AgentType"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal AgentType: %s", err)
	} else {
		v.AgentType = t
	}

	if err := json.Unmarshal(objmap["AgentIdentityToken"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal AgentIdentityToken: %s", err)
	} else {
		v.AgentIdentityToken = t
	}

	if err := json.Unmarshal(objmap["TargetId"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal TargetId: %s", err)
	} else {
		v.TargetId = t
	}

	if err := json.Unmarshal(objmap["IdpProvider"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal IdpProvider: %s", err)
	} else {
		v.IdpProvider = t
	}

	if err := json.Unmarshal(objmap["IdpOrgId"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal IdpOrgId: %s", err)
	} else {
		v.IdpOrgId = t
	}

	if err := json.Unmarshal(objmap["ShutdownReason"], &t); err != nil {
		return fmt.Errorf("failed to unmarshal ShutdownReason: %s", err)
	} else {
		v.ShutdownReason = t
	}

	var privateKey *keypair.PrivateKey
	if err := json.Unmarshal(objmap["PrivateKey"], &privateKey); err != nil {
		return fmt.Errorf("failed to unmarshal privateKey: %s", err)
	} else {
		v.PrivateKey = privateKey
	}

	var publicKey *keypair.PublicKey
	if err := json.Unmarshal(objmap["PublicKey"], &publicKey); err != nil {
		return fmt.Errorf("failed to unmarshal publicKey: %s", err)
	} else {
		v.PublicKey = publicKey
	}

	// Our old shutdown state was saved as a string via fmt.Sprintf. We just ignore those
	// old states because if this code is reading such a state, then the user just updated
	// their agent which is not restart we need to report.
	val := objmap["ShutdownState"]

	if string(val) == "null" || string(val) == `""` {
		return nil
	}

	var shutdownState map[string]string
	var legacyStateError *json.UnmarshalTypeError
	if err := json.Unmarshal([]byte(val), &shutdownState); errors.As(err, &legacyStateError) {
		v.ShutdownState = make(map[string]string)
	} else if err != nil {
		return fmt.Errorf("failed to unmarshal shutdown state %s: %s", string(val), err)
	} else {
		v.ShutdownState = shutdownState
	}

	return nil
}

// This is a bit of history keeping but it's also very useful to keep track of past definitions for this
// object so that we can test backwards compatability
// This version covers the structure prior to https://github.com/bastionzero/bzero/pull/169
// There were changes to this structure since the agent's inception, but in the above PR, we
// changed type definitions
type DataV1 struct {
	PublicKey          string
	PrivateKey         string
	ServiceUrl         string
	TargetName         string
	Namespace          string
	IdpProvider        string
	IdpOrgId           string
	TargetId           string
	EnvironmentId      string
	EnvironmentName    string
	AgentType          string
	Version            string
	ShutdownReason     string
	ShutdownState      string
	AgentIdentityToken string
}