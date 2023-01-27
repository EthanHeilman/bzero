package mrtap

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Masterminds/semver"
	orderedmap "github.com/wk8/go-ordered-map"

	"bastionzero.com/bctl/v1/bctl/daemon/mrtap/bzcert"
	bzerr "bastionzero.com/bctl/v1/bzerolib/error"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/message"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/util"
)

// Max number of times we will try to resend after an error message
const maxErrorRecoveryTries = 3

// The number of messages we're allowed to precalculate and send without having
// received an ack
const maxPipelineLimit = 8

type Mrtap struct {
	logger *logger.Logger

	bzcert bzcert.IDaemonBZCert

	agentPubKey  *keypair.PublicKey
	ackPublicKey *keypair.PublicKey

	synAction string

	// a channel for all the messages we give the datachannel to send
	outboxQueue chan *message.MrtapMessage

	// stateLock mutex coordinates usage of state variables defined below
	stateLock sync.Mutex
	// pipelineOpen allows concurrent goroutines to wait for some specific
	// condition of the stateLock protected state variables to be true
	pipelineOpen *sync.Cond
	// ordered hash map to keep track of sent MrTAP messages
	pipelineMap    *orderedmap.OrderedMap
	pipelineLength int

	// isHandshakeComplete is true when SynAck has been received. It is reset to
	// false during recovery
	isHandshakeComplete bool
	// not the last ack we've received but the last ack we've received
	lastAck *message.MrtapMessage
	// Data msg that was last acked by the agent
	lastAckedData *message.MrtapMessage
	// bool variable for letting the datachannel know when to start processing
	// incoming messages again
	recovering bool
	// keep track of how many times we've tried to recover
	errorRecoveryAttempt int
	// We set the schemaVersion to use based on the schemaVersion sent by the
	// agent in the synack
	schemaVersion      *semver.Version
	prePipeliningAgent bool
	pipelineLimit      int
}

func New(
	logger *logger.Logger,
	agentPubKey *keypair.PublicKey,
	bzcert bzcert.IDaemonBZCert,
) (*Mrtap, error) {

	mt := &Mrtap{
		logger:        logger,
		bzcert:        bzcert,
		agentPubKey:   agentPubKey,
		pipelineMap:   orderedmap.New(),
		outboxQueue:   make(chan *message.MrtapMessage, maxPipelineLimit),
		synAction:     "initial",
		pipelineLimit: maxPipelineLimit,
	}
	mt.pipelineOpen = sync.NewCond(&mt.stateLock)

	return mt, nil
}

func (m *Mrtap) IsPipelineEmpty() bool {
	return m.pipelineLength == 0
}

func (m *Mrtap) Recovering() bool {
	m.stateLock.Lock()
	defer m.stateLock.Unlock()
	return m.recovering
}

func (m *Mrtap) Release() {
	m.pipelineOpen.Broadcast()
}

func (m *Mrtap) Outbox() <-chan *message.MrtapMessage {
	return m.outboxQueue
}

func (m *Mrtap) Recover(errMessage bzerr.ErrorMessage) error {
	m.stateLock.Lock()
	defer m.stateLock.Unlock()

	// only recover from this error message if it corresponds to a message we've actually sent
	// our old error messages weren't setting hpointers correctly
	// TODO: CWC-1818: remove schema version check
	if errMessage.SchemaVersion != "" {
		if errMessage.HPointer == "" {
			return fmt.Errorf("error message hpointer empty")
		} else if pair := m.pipelineMap.GetPair(errMessage.HPointer); pair == nil && !m.recovering {
			m.logger.Infof("agent error does not correspond to a message sent by this datachannel")
			return nil // not a fatal error
		} else if m.recovering {
			m.logger.Infof("ignoring error message because we're already in recovery")
			return nil // not a fatal error
		}
	}

	if m.errorRecoveryAttempt >= maxErrorRecoveryTries {
		return fmt.Errorf("retried too many times to fix error: %s", errMessage.Message)
	} else {
		m.errorRecoveryAttempt++
		m.logger.Infof("Attempt #%d to recover from error: %s", m.errorRecoveryAttempt, errMessage.Message)
	}

	m.recovering = true
	if _, err := m.buildSyn("", []byte{}, true); err != nil {
		return err
	}
	return nil
}

func (m *Mrtap) resend(nonce string) {
	recoveryMap := *m.pipelineMap
	m.pipelineMap = orderedmap.New()

	// Check to see if we're talking with an agent that doesn't set SynAck's
	// nonce correctly
	//
	// TODO: CWC-2093: Remove this once all agents update
	synAckNonceConstraint, err := semver.NewConstraint("> 2.0")
	if err != nil {
		m.logger.Errorf("malformed version constraint: %s", err)
		return
	}

	shouldCheckNonce := synAckNonceConstraint.Check(m.schemaVersion)

	// figure out where we need to start resending from
	if pair := (&recoveryMap).GetPair(nonce); pair == nil {
		if shouldCheckNonce {
			// Get hash of last acked Data msg
			if m.lastAckedData == nil {
				m.logger.Info("Nothing to resend")
				return
			}
			lastAckedDataHash := m.lastAckedData.Hash()
			if lastAckedDataHash == "" {
				m.logger.Errorf("failed to hash the last ack'ed sent message")
				return
			}

			// Extra check to prevent a replay attack where adversary replays
			// messages in pipelineMap against another DC. See CWC-1940 for more
			// details.
			//
			// We can't always check this condition because old agents did not
			// set nonce correctly in SynAck.
			//
			// TODO: CWC-2093: Remove this check once all agents update, and
			// update code to always check the nonce.
			if nonce != lastAckedDataHash {
				m.logger.Errorf("Recovery message does not point to the last ack'd sent message")
				return
			}
		}

		// if the referenced message was acked, we won't have it in our map so
		// we assume we have to resend everything
		for lostPair := (&recoveryMap).Oldest(); lostPair != nil; lostPair = lostPair.Next() {
			mrtapMessage := lostPair.Value.(message.MrtapMessage)
			m.pipeline(mrtapMessage.GetAction(), mrtapMessage.GetActionPayload())
		}
	} else {
		// if the hpointer references a message that hasn't been acked, we assume the ack
		// dropped and resend all messages starting with the one immediately AFTER the one
		// referenced by the hpointer
		for lostPair := pair.Next(); lostPair != nil; lostPair = lostPair.Next() {
			mrtapMessage := lostPair.Value.(message.MrtapMessage)
			m.pipeline(mrtapMessage.GetAction(), mrtapMessage.GetActionPayload())
		}
	}
}

func (m *Mrtap) Validate(mrtapMessage *message.MrtapMessage) error {
	// TODO: CWC-1553: Remove this code once all agents have updated
	if msg, ok := mrtapMessage.Payload.(message.SynAckPayload); ok && m.ackPublicKey == nil {
		if publickey, err := keypair.PublicKeyFromString(msg.TargetPublicKey); err != nil {
			return fmt.Errorf("invalid public key")
		} else {
			m.ackPublicKey = publickey
		}
	}

	// Verify the agent's signature
	if err := mrtapMessage.VerifySignature(m.agentPubKey); err != nil {
		// TODO: CWC-1553: Remove this inner conditional once all agents have updated
		if innerErr := mrtapMessage.VerifySignature(m.ackPublicKey); innerErr != nil {
			return fmt.Errorf("%w: failed to verify %v signature: inner error: %s outer error: %s", ErrInvalidSignature, mrtapMessage.Type, innerErr, err)
		}
	}

	hpointer, err := mrtapMessage.GetHpointer()
	if err != nil {
		return err
	}

	m.stateLock.Lock()
	defer m.stateLock.Unlock()

	// Check this messages is in response to one we've sent
	if ackedMsg, ok := m.pipelineMap.Get(hpointer); ok {
		switch mrtapMessage.Type {
		case message.SynAck:
			if msg, ok := mrtapMessage.Payload.(message.SynAckPayload); ok {
				m.lastAck = mrtapMessage
				m.pipelineMap.Delete(hpointer) // delete syn from map

				// Must set schema version first in case we're recovering and
				// resend() has to rebuild Data messages. If we don't set
				// schemaVersion first, then the resent Data messages will refer
				// to the previously agreed schema version (in the original
				// handshake prior to recovery) which might be different.
				parsedSchemaVersion, err := semver.NewVersion(msg.SchemaVersion)
				if err != nil {
					return ErrFailedToParseVersion
				}
				m.schemaVersion = parsedSchemaVersion

				// when we recover, we're recovering based on the nonce in the syn/ack because unless
				// it's not in response to the initial syn, where the nonce is a true random number,
				// it is an hpointer which refers to the agent's last received and validated message.
				// aka it is the current state of the MrTAP hash chain according to the agent and this
				// recovery mechanism allows us to sync our MrTAP state to that
				m.recovering = false
				m.resend(msg.Nonce)

				// check to see if we're talking with an agent that's using
				// pre-2.0 MrTAP because we'll need to dirty the payload
				// by adding extra quotes around it TODO: CWC-1820: remove once
				// all daemon's are updated
				if c, err := semver.NewConstraint("< 2.0"); err != nil {
					return fmt.Errorf("unable to create versioning constraint")
				} else {
					m.prePipeliningAgent = c.Check(parsedSchemaVersion)

					if m.prePipeliningAgent {
						// Override default
						m.pipelineLimit = 1
					}
				}

				// We've received a SynAck, so the handshake is complete
				m.isHandshakeComplete = true
			}
		case message.DataAck:
			m.lastAck = mrtapMessage
			m.pipelineMap.Delete(hpointer)

			// Store reference to last acked Data msg
			ackedDataMsg := ackedMsg.(message.MrtapMessage)
			m.lastAckedData = &ackedDataMsg

			// If we're here, it means that the previous data message that
			// caused the error was accepted
			m.errorRecoveryAttempt = 0
		}

		// Condition variable changed. We must call Broadcast() to prevent deadlock
		m.pipelineLength = m.pipelineMap.Len()
		m.pipelineOpen.Broadcast()
	} else {
		return fmt.Errorf("%w: %T message did not correspond to a previously sent message", ErrUnknownHPointer, mrtapMessage.Payload)
	}

	return nil
}

func (m *Mrtap) Inbox(action string, actionPayload []byte) error {
	m.stateLock.Lock()
	defer m.stateLock.Unlock()

	// Wait if pipeline is full OR if handshake is not complete
	for m.pipelineMap.Len() >= m.pipelineLimit || !m.isHandshakeComplete {
		m.logger.Debugf("Pipeline full: %t, Handshake complete: %t. Waiting to send next message...", m.pipelineMap.Len() >= m.pipelineLimit, m.isHandshakeComplete)
		m.pipelineOpen.Wait()
	}

	return m.pipeline(action, actionPayload)
}

func (m *Mrtap) pipeline(action string, actionPayload []byte) error {
	if action == "" {
		return fmt.Errorf("i'm not allowed to build a MrTAP message with empty action")
	}

	// get the ack we're going to be building our new message off of
	var ack *message.MrtapMessage
	if pair := m.pipelineMap.Newest(); pair == nil {
		// if our pipeline map is empty, we build off our last received ack
		if m.lastAck != nil {
			ack = m.lastAck
		} else {
			return fmt.Errorf("can't build message because there's nothing to build it off of")
		}
	} else {
		// otherwise, we're going to need to predict the ack we're building off of
		mrtapMessage := pair.Value.(message.MrtapMessage)
		if newAck, err := mrtapMessage.BuildUnsignedDataAck([]byte{}, m.agentPubKey.String(), m.schemaVersion.String()); err != nil {
			return fmt.Errorf("failed to predict ack: %s", err)
		} else {
			ack = &newAck
		}
	}

	// build our new data message and then ship it!
	if newMessage, err := m.buildResponse(ack, action, actionPayload); err != nil {
		return fmt.Errorf("failed to build new message: %w", err)
	} else if err := m.addToPipelineMap(newMessage); err != nil {
		return err
	} else {
		m.outboxQueue <- &newMessage
		return nil
	}
}

func (m *Mrtap) buildResponse(mrtapMessage *message.MrtapMessage, action string, payload []byte) (message.MrtapMessage, error) {
	// TODO: CWC-1820: remove this if statement once all daemon's are updated
	if m.prePipeliningAgent {
		// if we're talking with an old agent, then we have to add extra quotes

		// sometimes go will extra marshal big things, but because we need to compensate for an old
		// extra marshaling bug on our part, we have to make sure that we are marshaling things the
		// correct number of times which means that we have to unmarshal the things that got extra
		// marshaled and then fancy marshal them in the special broken way we have to reproduce for
		// backwards compatability with old agents
		var preMarshal []byte
		if err := json.Unmarshal(payload, &preMarshal); err == nil {
			payload = preMarshal
		}

		encoded := base64.StdEncoding.EncodeToString(payload)
		payload, _ = json.Marshal(string(encoded))
	}

	// Use the agreed upon schema version from the synack when building data messages
	if responseMessage, err := mrtapMessage.BuildUnsignedData(action, payload, m.bzcert.Hash(), m.schemaVersion.String()); err != nil {
		return responseMessage, err
	} else if ok := responseMessage.Sign(m.bzcert.PrivateKey()); !ok {
		return responseMessage, fmt.Errorf("%w: %s", ErrFailedToSign, err)
	} else {
		return responseMessage, nil
	}
}

func (m *Mrtap) addToPipelineMap(mrtapMessage message.MrtapMessage) error {
	if hash := mrtapMessage.Hash(); hash == "" {
		return fmt.Errorf("failed to hash message")
	} else {
		m.pipelineMap.Set(hash, mrtapMessage)
		m.pipelineLength = m.pipelineMap.Len()
		return nil
	}
}

func (m *Mrtap) BuildSyn(action string, payload interface{}, send bool) (*message.MrtapMessage, error) {
	m.stateLock.Lock()
	defer m.stateLock.Unlock()

	return m.buildSyn(action, payload, send)
}

// It is the caller's responsibility to lock the stateLock mutex before calling this function
func (m *Mrtap) buildSyn(action string, payload interface{}, send bool) (*message.MrtapMessage, error) {
	// Reset state
	m.isHandshakeComplete = false
	m.lastAck = nil

	// Refresh our BZCert before rebuilding the syn in case the cert expired.
	// This may still fail if the initialId Token is no longer valid
	if err := m.bzcert.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to refresh BastionZero certificate: %w", err)
	}

	if m.synAction == "initial" {
		m.synAction = action
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal action params")
	}

	// Build the MrTAP message
	synPayload := message.SynPayload{
		SchemaVersion: message.SchemaVersion,
		Type:          string(message.Syn),
		Action:        m.synAction,
		ActionPayload: payloadBytes,
		TargetId:      m.agentPubKey.String(),
		Nonce:         util.Nonce(),
		BZCert:        *m.bzcert.Cert(),
	}

	mrtapMessage := message.MrtapMessage{
		Type:    message.Syn,
		Payload: synPayload,
	}

	// Sign it and add it to our hash map
	if ok := mrtapMessage.Sign(m.bzcert.PrivateKey()); !ok {
		return nil, fmt.Errorf("%s: %w", ErrFailedToSign, err)
	} else if err := m.addToPipelineMap(mrtapMessage); err != nil {
		return nil, err
	} else {
		if send {
			m.outboxQueue <- &mrtapMessage
		}
		return &mrtapMessage, nil
	}
}
