package datachannel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tomb "gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/daemon/mrtap"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	bzerr "bastionzero.com/bctl/v1/bzerolib/error"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/bzcert"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/message"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	"bastionzero.com/bctl/v1/bzerolib/plugin/db"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
	"bastionzero.com/bctl/v1/bzerolib/unix/unixuser"
)

const (
	// amount of time we're willing to wait for our first MrTAP message
	handshakeTimeout = time.Minute // TODO: Decrease

	// maximum amount of time we want to keep this datachannel alive after
	// neither receiving nor sending anything
	datachannelIdleTimeout = 7 * 24 * time.Hour
)

type OpenDataChannelPayload struct {
	Syn    []byte `json:"syn"`
	Action string `json:"action"`
}

type IKeysplitting interface {
	BuildSyn(action string, payload interface{}, send bool) (*message.MrtapMessage, error)
	Validate(mrtapMessage *message.MrtapMessage) error

	Recover(errorMessage bzerr.ErrorMessage) error
	Inbox(action string, actionPayload []byte) error
	IsPipelineEmpty() bool
	Outbox() <-chan *message.MrtapMessage
	Release()
	Recovering() bool
}

type IPlugin interface {
	ReceiveMrtap(action string, actionPayload []byte) error
	ReceiveStream(smessage smsg.StreamMessage)
	Outbox() <-chan bzplugin.ActionWrapper
	Done() <-chan struct{}
	Err() error
	Kill(err error)
}

type DataChannel struct {
	tmb    tomb.Tomb
	logger *logger.Logger
	id     string // DataChannel's ID

	conn   connection.Connection
	mrtap  *mrtap.Mrtap
	plugin IPlugin

	// channels for incoming messages
	inputChan chan *am.AgentMessage

	// whether or not to wait for the inputChannel queue to be flushed before exiting
	processInputChanBeforeExit bool
}

func New(
	logger *logger.Logger,
	id string,
	conn connection.Connection,
	mrtap *mrtap.Mrtap,
	plugin IPlugin,
	action string,
	synPayload interface{},
	attach bool, // bool to indicate if we are attaching to an existing datachannel
	processInputChanBeforeExit bool,
) (*DataChannel, error) {

	dc := &DataChannel{
		logger:                     logger,
		id:                         id,
		conn:                       conn,
		mrtap:                      mrtap,
		plugin:                     plugin,
		inputChan:                  make(chan *am.AgentMessage, 50),
		processInputChanBeforeExit: processInputChanBeforeExit,
	}

	// register with connection so datachannel can send and receive messages
	conn.Subscribe(id, dc)

	dc.tmb.Go(func() error {
		var err error
		defer func() {
			dc.logger.Infof("sending CloseDataChannel message to the agent")
			conn.Send(am.AgentMessage{
				ChannelId:   dc.id,
				MessageType: am.CloseDataChannel,
			})
			dc.logger.Info("Datachannel done")
		}()

		go dc.sendMrtap()
		go dc.zapPluginOutput()

		// wait for the syn/ack to our intial syn message or an error
		dc.logger.Debugf("wait for the syn/ack to our intial syn message or an error")
		if err = dc.handshakeOrTimeout(attach, action, synPayload); err != nil {
			dc.logger.Error(err)
			return err
		}
		dc.logger.Info("Initial handshake complete")

		for {
			select {
			case <-dc.tmb.Dying():
				dc.logger.Infof("Datachannel dying: %s", dc.tmb.Err())
				dc.plugin.Kill(dc.tmb.Err())
				return nil
			case <-dc.plugin.Done():
				dc.logger.Infof("%s is done", action)
				if processInputChanBeforeExit {
					// wait for any in-flight messages to come in and ensure all outgoing messages go out
					dc.waitForRemainingMessages()
				}
				return dc.plugin.Err()
			case agentMessage := <-dc.inputChan: // receive messages
				if err := dc.processInputMessage(agentMessage); err != nil {
					dc.logger.Error(err)
				}
			case <-time.After(datachannelIdleTimeout):
				dc.logger.Info("Datachannel has been idle for too long, ceasing operation")
				return fmt.Errorf("cleaning up stale datachannel")
			}
		}
	})

	return dc, nil
}

func (d *DataChannel) handshakeOrTimeout(attach bool, action string, synPayload interface{}) error {
	maxRetry := 3
	retryCount := 0

	// This will initialize our handshake by either attaching to an existing
	// datachannel (and sending a lone syn) OR by creating a new datachannel on the
	// agent by sending the syn as part of a OpenDataChannel request
	if err := d.start(attach, action, synPayload); err != nil {
		return err
	}

	d.logger.Info("Waiting for handshake to complete")
	start := time.Now()
	for {
		select {
		case <-d.tmb.Dying():
			return nil

		case agentMessage := <-d.inputChan:
			switch am.MessageType(agentMessage.MessageType) {
			case am.Error:
				d.logger.Errorf("Received error message on initial syn: %s", string(agentMessage.MessagePayload))

				// Limit the number of times we retry to initiate handshake; it is very likely
				// this error will be unrecoverable
				if retryCount >= maxRetry {
					rerr := fmt.Sprintf("retried %d times to recover from error on initial syn without success", maxRetry)
					var errMessage bzerr.ErrorMessage
					if err := json.Unmarshal(agentMessage.MessagePayload, &errMessage); err != nil {
						d.logger.Errorf("failed to unmarshal error message: %s", err)
					} else if err := checkForKnownErrors(errMessage.Message); err != nil {
						return err
					} else {
						// base case, we couldn't unmarshal or it's not a known error
						rerr += fmt.Sprintf("; Latest error: %s", errMessage.Message)
					}

					d.logger.Errorf(rerr)
					return fmt.Errorf(rerr)
				}
				retryCount++

				// If we get an error on that first syn, we have to restart the flow
				if err := d.start(attach, action, synPayload); err != nil {
					return err
				}
			case am.Mrtap:
				// log the time it took to complete the handshake
				diff := time.Since(start)
				d.logger.Infof("It took %s to complete handshake", diff.Round(time.Millisecond).String())

				if err := d.handleMrtap(agentMessage); err != nil {
					return err
				} else {
					return nil
				}
			default:
				return fmt.Errorf("datachannel must start with a MrTAP or error message, received: %s", agentMessage.MessageType)
			}
		case <-time.After(handshakeTimeout):
			return fmt.Errorf("handshake timed out")
		}
	}
}

func (d *DataChannel) waitForRemainingMessages() {
	checkOutboxInterval := time.Second
	absoluteTimeout := time.NewTimer(10 * time.Second)
	defer absoluteTimeout.Stop()
	for {
		select {
		// even if the plugin says it's done, we need to keep processing acks from the agent
		case agentMessage := <-d.inputChan:
			if err := d.processInputMessage(agentMessage); err != nil {
				d.logger.Error(err)
			}
		case <-time.After(checkOutboxInterval):
			d.logger.Infof("Checking for any remaining messages: outbox: %d, pipeline empty: %t", len(d.plugin.Outbox()), d.mrtap.IsPipelineEmpty())
			// if the plugin has nothing pending and the pipeline is empty, we can safely stop
			if len(d.plugin.Outbox()) == 0 && d.mrtap.IsPipelineEmpty() {
				return
			}
			// there are cases, such as during an iperf download, when the agent-side plugin closes
			// and thus stops sending acks. In this case, the pipeline does not empty completely,
			// creating the need for an escape hatch
		case <-absoluteTimeout.C:
			d.logger.Errorf("timed out waiting for agent to finish sending messages after plugin closed")
			return
		}
	}
}

func (d *DataChannel) sendMrtap() error {
	for {
		select {
		case <-d.tmb.Dying():
			d.mrtap.Release()
			return nil
		case mrtapMessage := <-d.mrtap.Outbox():
			if mrtapMessage.Type == message.Syn || !d.mrtap.Recovering() {
				d.logger.Infof("Sending a MrTAP %s message", mrtapMessage.Type)
				// TODO: CWC-2183; we still send a legacy message to accommodate older agents. Newer ones can handle either
				d.send(am.MrtapLegacy, mrtapMessage)
			}
		}
	}
}

func (d *DataChannel) zapPluginOutput() error {
	for {
		select {
		case <-d.tmb.Dying():
			return nil
		case wrapper := <-d.plugin.Outbox():
			// Build and send response
			if err := d.mrtap.Inbox(wrapper.Action, wrapper.ActionPayload); err != nil {
				d.logger.Errorf("could not build response message: %s", err)
			}
		}
	}
}

func (d *DataChannel) Done() <-chan struct{} {
	return d.tmb.Dead()
}

func (d *DataChannel) Err() error {
	return d.tmb.Err()
}

func (d *DataChannel) Close(reason error) {
	if !d.tmb.Alive() {
		return
	}
	d.tmb.Kill(reason) // kills all datachannel, plugin, and action goroutines
	d.tmb.Wait()
}

func (d *DataChannel) start(attach bool, action string, synPayload interface{}) error {
	// if we're attaching to an existing datachannel vs if we are creating a new one
	if !attach {
		// tell Bastion we're opening a datachannel and send SYN to agent initiates an authenticated datachannel
		d.logger.Info("Sending request to agent to open a new datachannel")
		return d.openDataChannel(action, synPayload)
	}

	if _, err := d.mrtap.BuildSyn(action, synPayload, true); err != nil {
		return fmt.Errorf("failed to build and send syn for attachment flow")
	} else {
		d.logger.Infof("Sending SYN on existing datachannel %s with action %s", d.id, action)
		return nil
	}
}

func (d *DataChannel) openDataChannel(action string, synPayload interface{}) error {
	synMessage, err := d.mrtap.BuildSyn(action, synPayload, false)
	if err != nil {
		return fmt.Errorf("error building syn: %w", err)
	}

	// Marshal the syn
	synBytes, err := json.Marshal(synMessage)
	if err != nil {
		return fmt.Errorf("error marshalling syn: %w", err)
	}

	messagePayload := OpenDataChannelPayload{
		Syn:    synBytes,
		Action: action,
	}

	// Marshal the messagePayload
	messagePayloadBytes, err := json.Marshal(messagePayload)
	if err != nil {
		return fmt.Errorf("error marshalling OpenDataChannelPayload: %w", err)
	}

	// send new datachannel message to agent, as we can build the syn here
	odMessage := am.AgentMessage{
		ChannelId:      d.id,
		MessagePayload: messagePayloadBytes,
		MessageType:    am.OpenDataChannel,
	}
	d.conn.Send(odMessage)

	return nil
}

// Wraps and sends the payload
func (d *DataChannel) send(messageType am.MessageType, messagePayload interface{}) error {
	if messageBytes, err := json.Marshal(messagePayload); err != nil {
		return fmt.Errorf("failed to marshal the provided agent message payload: %s", messageBytes)
	} else {
		agentMessage := am.AgentMessage{
			ChannelId:      d.id,
			MessageType:    messageType,
			SchemaVersion:  am.CurrentVersion,
			MessagePayload: messageBytes,
		}

		// Push message to connection channel output
		d.conn.Send(agentMessage)
		return nil
	}
}

func (d *DataChannel) Receive(agentMessage am.AgentMessage) {
	if d.tmb.Alive() {
		d.inputChan <- &agentMessage
	}
}

func (d *DataChannel) processInputMessage(agentMessage *am.AgentMessage) error {
	d.logger.Debugf("Datachannel received %v message", agentMessage.MessageType)

	switch am.MessageType(agentMessage.MessageType) {
	case am.Error:
		if err := d.handleError(agentMessage); err != nil {
			// if we can't recover then shut everything down
			d.logger.Error(err)
			d.tmb.Kill(err)
		}
	case am.Mrtap:
		if err := d.handleMrtap(agentMessage); err != nil {
			d.logger.Error(err)
		}
	case am.Stream:
		return d.handleStream(agentMessage)
	default:
		return fmt.Errorf("unhandled message type: %s", agentMessage.MessageType)
	}
	return nil
}

func (d *DataChannel) handleError(agentMessage *am.AgentMessage) error {
	var errMessage bzerr.ErrorMessage
	if err := json.Unmarshal(agentMessage.MessagePayload, &errMessage); err != nil {
		return fmt.Errorf("could not unmarshal error message: %s", err)
	}

	if bzerr.ErrorType(errMessage.Type) == bzerr.MrtapValidationError {
		return d.mrtap.Recover(errMessage)
	} else if err := checkForKnownErrors(errMessage.Message); err != nil {
		return err
	}

	// return any error we don't specifically handle
	return fmt.Errorf("received fatal %s error from agent: %s", errMessage.Type, errMessage.Message)
}

func (d *DataChannel) handleStream(agentMessage *am.AgentMessage) error {
	var sMessage smsg.StreamMessage
	if err := json.Unmarshal(agentMessage.MessagePayload, &sMessage); err != nil {
		return fmt.Errorf("malformed Stream message")
	} else {
		d.plugin.ReceiveStream(sMessage)
		return nil
	}
}

func (d *DataChannel) handleMrtap(agentMessage *am.AgentMessage) error {
	// unmarshal the MrTAP message
	d.logger.Debugf("Handling MrTAP message")
	var mrtapMessage message.MrtapMessage
	if err := json.Unmarshal(agentMessage.MessagePayload, &mrtapMessage); err != nil {
		return fmt.Errorf("malformed MrTAP message")
	}

	// validate MrTAP message
	if err := d.mrtap.Validate(&mrtapMessage); err != nil {
		return fmt.Errorf("invalid MrTAP message: %s", err)
	}

	switch mrtapMessage.Payload.(type) {
	case message.SynAckPayload:
	case message.DataAckPayload:
		// Send message to plugin's input message handler
		if err := d.plugin.ReceiveMrtap(mrtapMessage.GetAction(), mrtapMessage.GetActionPayload()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unhandled MrTAP type")
	}

	return nil
}

// Sometimes the agent produces an error that we would like to handle specially (e.g., show the user a helpful message).
// Unfortunately these errors' types get lost when they are serialized and sent across the wire.
// although string comparison is a brittle way to check for such errors, it's the best tool we've got
func checkForKnownErrors(errString string) error {
	if strings.Contains(errString, unixuser.UserNotFoundErrMsg) {
		return &unixuser.UserNotFoundError{}
	}

	if strings.Contains(errString, bzcert.ServiceAccountNotConfiguredMsg) {
		serviceAccountConfigErrTokenized := strings.Split(errString, bzcert.ServiceAccountNotConfiguredMsg)
		serviceAccountConfigErr := serviceAccountConfigErrTokenized[len(serviceAccountConfigErrTokenized)-1]
		return &bzcert.ServiceAccountError{InnerError: fmt.Errorf(serviceAccountConfigErr)}
	}

	// The below errors are bundled together in a specific order to be able to prioritize specific, wrapped errors from
	// higher-level generic ones
	if strings.Contains(errString, db.ClientCertCosignErrorString) {
		return &db.ClientCertCosignError{}
	} else if strings.Contains(errString, db.TLSDisabledErrorString) {
		return &db.TLSDisabledError{}
	} else if strings.Contains(errString, db.ConnectionRefusedString) {
		return &db.ConnectionRefusedError{}
	} else if strings.Contains(errString, db.UnrecognizedRootCertErrorString) {
		return &db.PwdbUnknownAuthorityError{}
	} else if strings.Contains(errString, db.ServerCertificateExpiredString) {
		return db.NewServerCertificateExpired(fmt.Errorf(errString))
	} else if strings.Contains(errString, db.IncorrectServerNameString) {
		return db.NewIncorrectServerName(fmt.Errorf(errString))
	} else if strings.Contains(errString, db.ConnectionFailedErrorString) {
		return &db.ConnectionFailedError{}
	}

	if strings.Contains(errString, db.PwdbMissingKeyErrorString) {
		return &db.PwdbMissingKeyError{}
	}

	// base case, we didn't find anything special
	return nil
}
