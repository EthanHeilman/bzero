package mrtap

import (
	"fmt"

	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/logger"
	bzcrt "bastionzero.com/bzerolib/mrtap/bzcert"
	"bastionzero.com/bzerolib/mrtap/message"
	"bastionzero.com/bzerolib/mrtap/util"
	"github.com/Masterminds/semver"
)

// schema version <= this value doesn't set targetId to the agent's pubkey
const schemaVersionTargetIdNotSet string = "1.0"

type Mrtap struct {
	logger           *logger.Logger
	lastDataMessage  *message.MrtapMessage
	expectedHPointer string
	clientBZCert     *bzcrt.BZCert // only for one client
	clientPublicKey  *keypair.PublicKey
	publickey        *keypair.PublicKey
	privatekey       *keypair.PrivateKey
	idpProvider      string
	idpOrgId         string
	serviceAccounts  []string

	// define constraints based on schema version
	shouldCheckTargetId *semver.Constraints

	daemonSchemaVersion *semver.Version
}

type MrtapConfig interface {
	GetPublicKey() *keypair.PublicKey
	GetPrivateKey() *keypair.PrivateKey
	GetIdpProvider() string
	GetIdpOrgId() string
	GetServiceAccountJwksUrls() []string
}

func New(logger *logger.Logger, config MrtapConfig) (*Mrtap, error) {
	shouldCheckTargetIdConstraint, err := semver.NewConstraint(fmt.Sprintf("> %s", schemaVersionTargetIdNotSet))
	if err != nil {
		return nil, fmt.Errorf("failed to create check target id constraint: %w", err)
	}

	return &Mrtap{
		logger:              logger,
		publickey:           config.GetPublicKey(),
		privatekey:          config.GetPrivateKey(),
		idpProvider:         config.GetIdpProvider(),
		idpOrgId:            config.GetIdpOrgId(),
		serviceAccounts:     config.GetServiceAccountJwksUrls(),
		shouldCheckTargetId: shouldCheckTargetIdConstraint,
	}, nil
}

func (m *Mrtap) Validate(msg *message.MrtapMessage) error {
	switch msg.Type {
	case message.Syn:
		synPayload := msg.Payload.(message.SynPayload)
		bzcert := synPayload.BZCert

		// Verify the BZCert
		if err := bzcert.Verify(m.idpProvider, m.idpOrgId, m.serviceAccounts); err != nil {
			return fmt.Errorf("failed to verify SYN's BZCert: %w", err)
		}

		if pubkey, err := keypair.PublicKeyFromString(bzcert.ClientPublicKey); err != nil {
			return fmt.Errorf("malformatted public key: %s", bzcert.ClientPublicKey)
		} else {
			m.clientPublicKey = pubkey
		}

		// Verify the signature
		if err := msg.VerifySignature(m.clientPublicKey); err != nil {
			return fmt.Errorf("failed to verify SYN's signature: %w", err)
		}

		// Extract semver version to determine if different protocol checks must be done
		v, err := semver.NewVersion(synPayload.SchemaVersion)
		if err != nil {
			return fmt.Errorf("failed to parse schema version (%v) as semver: %w", synPayload.SchemaVersion, err)
		} else {
			m.daemonSchemaVersion = v
		}

		// Daemons with schema version <= 1.0 do not set targetId, so we cannot
		// apply this check universally
		// TODO: CWC-1553: Always check TargetId once all daemons have updated
		if m.shouldCheckTargetId.Check(v) {
			// Verify SYN message commits to this agent's cryptographic identity
			if synPayload.TargetId != m.publickey.String() {
				return fmt.Errorf("SYN's TargetId did not match agent's public key")
			}
		}

		m.clientBZCert = &bzcert
	case message.Data:
		dataPayload := msg.Payload.(message.DataPayload)

		// Check BZCert matches one we have stored
		if m.clientBZCert.Hash() != dataPayload.BZCertHash {
			return fmt.Errorf("DATA's BZCert does not match the active user's")
		}

		// Verify the signature
		if err := msg.VerifySignature(m.clientPublicKey); err != nil {
			return err
		}

		// Check that BZCert isn't expired
		if m.clientBZCert.Expired() {
			return fmt.Errorf("DATA's referenced BZCert has expired")
		}

		// Verify received hash pointer matches expected
		if dataPayload.HPointer != m.expectedHPointer {
			return fmt.Errorf("DATA's hash pointer %s did not match expected hash pointer %s", dataPayload.HPointer, m.expectedHPointer)
		}

		m.lastDataMessage = msg
	default:
		return fmt.Errorf("error validating unhandled MrTAP type")
	}

	return nil
}

func (m *Mrtap) BuildAck(msg *message.MrtapMessage, action string, actionPayload []byte) (message.MrtapMessage, error) {
	var responseMessage message.MrtapMessage
	var err error

	schemaVersion, err := m.getSchemaVersionToUse()
	if err != nil {
		return responseMessage, err
	}

	switch msg.Type {
	case message.Syn:
		// If this is the beginning of the hash chain, then we create a nonce with a random value,
		// otherwise we use the hash of the previous value to maintain the hash chain and immutability
		nonce := util.Nonce()
		if m.lastDataMessage != nil {
			if lastDataMessageHash := m.lastDataMessage.Hash(); lastDataMessageHash == "" {
				return message.MrtapMessage{}, fmt.Errorf("failed to get hash of last valid data message")
			} else {
				nonce = lastDataMessageHash
			}
		}

		responseMessage, err = msg.BuildUnsignedSynAck(actionPayload, m.publickey.String(), nonce, schemaVersion.String())

	case message.Data:
		responseMessage, err = msg.BuildUnsignedDataAck(actionPayload, m.publickey.String(), schemaVersion.String())
	default:

	}

	if err != nil {
		return responseMessage, err
	}

	responseMessage.Sign(m.privatekey)
	if responseMessage.Signature == "" {
		return responseMessage, fmt.Errorf("could not sign payload: %s", err)
	} else if hash := responseMessage.Hash(); hash == "" {
		return responseMessage, fmt.Errorf("could not hash payload")
	} else {
		m.expectedHPointer = hash
		return responseMessage, nil
	}
}

func (m *Mrtap) getSchemaVersionToUse() (*semver.Version, error) {
	agentVersion, err := semver.NewVersion(message.SchemaVersion)
	if err != nil {
		return nil, err
	}

	if m.daemonSchemaVersion.LessThan(agentVersion) {
		return m.daemonSchemaVersion, nil
	} else {
		return agentVersion, nil
	}
}
