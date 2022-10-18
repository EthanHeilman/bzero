package vault

import (
	"encoding/json"
	"fmt"
	"strings"
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
	PublicKey  string
	PrivateKey string

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

// In order to make the new vault backwards compatible, we have to have some custom
// unmarshalling logic
func (v *vault) UnmarshalJSON(data []byte) error {
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

	// We've changed the types of these fields from string to something more specific.
	// Because of that we need to peel of any leading/trailing quotation marks that belie
	// the variables' true type
	var privateKey string
	if err := json.Unmarshal(objmap["PrivateKey"], &privateKey); err != nil {
		return fmt.Errorf("failed to unmarshal privateKey: %s", err)
	} else {
		v.PrivateKey = privateKey
	}

	var publicKey string
	if err := json.Unmarshal(objmap["PublicKey"], &publicKey); err != nil {
		return fmt.Errorf("failed to unmarshal publicKey: %s", err)
	} else {
		v.PublicKey = publicKey
	}

	// Our old shutdown state was saved as a string via fmt.Sprintf which we need to undo
	// in order for us to be able to parse it as a map
	val := objmap["ShutdownState"]

	if string(data) == "null" || string(data) == `""` {
		v.ShutdownState = map[string]string{}
	}

	s := strings.TrimSuffix(strings.TrimPrefix(string(val), `"`), `"`)
	if strings.HasPrefix(s, "map[") {
		s = strings.TrimPrefix(s, "map[")
		s = strings.TrimSuffix(s, "]")
		s = strings.ReplaceAll(s, ":", `":"`)
		s = strings.ReplaceAll(s, " ", `", "`)
		s = `{"` + s + `"}`
		val = json.RawMessage(s)
	}

	var shutdownState map[string]string
	if err := json.Unmarshal([]byte(val), &shutdownState); err != nil {
		return fmt.Errorf("failed to unmarshal shutdown state %s: %s", string(val), err)
	} else {
		v.ShutdownState = shutdownState
	}

	return nil
}
