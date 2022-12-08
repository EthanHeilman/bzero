package datachannel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver"
	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/agent/plugin/db"
	"bastionzero.com/bctl/v1/bctl/agent/plugin/kube"
	"bastionzero.com/bctl/v1/bctl/agent/plugin/shell"
	"bastionzero.com/bctl/v1/bctl/agent/plugin/ssh"
	"bastionzero.com/bctl/v1/bctl/agent/plugin/web"

	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	bzerror "bastionzero.com/bctl/v1/bzerolib/error"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/mrtap/message"
	bzplugin "bastionzero.com/bctl/v1/bzerolib/plugin"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

const (
	closeTimeout = 10 * time.Second
)

type IMrtap interface {
	Validate(mrtapMessage *message.MrtapMessage) error
	BuildAck(mrtapMessage *message.MrtapMessage, action string, actionPayload []byte) (message.MrtapMessage, error)
}

type IPlugin interface {
	Receive(action string, actionPayload []byte) ([]byte, error)
	Done() <-chan struct{}
	Kill()
}

type DataChannel struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	id string

	conn   connection.Connection
	mrtap  IMrtap
	plugin IPlugin

	// incoming and outgoing message channels
	inputChan  chan am.AgentMessage
	outputChan chan am.AgentMessage

	// backward compatability code for when the payload used to come with extra quotes
	payloadClean bool
}

func New(
	parentTmb *tomb.Tomb,
	logger *logger.Logger,
	conn connection.Connection,
	mrtap IMrtap,
	id string,
	syn []byte,
) (*DataChannel, error) {

	datachannel := &DataChannel{
		logger:     logger,
		id:         id,
		conn:       conn,
		mrtap:      mrtap,
		inputChan:  make(chan am.AgentMessage, 50),
		outputChan: make(chan am.AgentMessage, 10),
	}

	// register with connection so datachannel can send a receive messages
	conn.Subscribe(id, datachannel)

	// validate the Syn message
	var synPayload message.MrtapMessage
	if err := json.Unmarshal([]byte(syn), &synPayload); err != nil {
		return nil, fmt.Errorf("malformed MrTAP message: %s", err)
	} else if synPayload.Type != message.Syn {
		return nil, fmt.Errorf("datachannel must be started with a SYN message")
	}

	// process our syn to startup the plugin
	if err := datachannel.handleMrtapMessage(&synPayload); err != nil {
		// Flush output channel messages to send any MrTAP errors that might have occurred
		datachannel.flushAllOutputChannelMessages()
		return nil, err
	}

	// listener for incoming messages
	datachannel.tmb.Go(func() error {
		defer logger.Infof("Datachannel is dead")
		defer datachannel.flushAllOutputChannelMessages()

		datachannel.tmb.Go(func() error {
			for {
				select {
				case <-datachannel.tmb.Dying():
					return nil
				case agentMessage := <-datachannel.inputChan: // receive messages
					datachannel.processInput(agentMessage)
				}
			}
		})

		for {
			select {
			case <-parentTmb.Dying(): // control channel is dying
				datachannel.plugin.Kill()
				return errors.New("agent was orphaned too young and can't be batman :'(")
			case <-datachannel.tmb.Dying():
				logger.Infof("datachannel is dying...killing plugin")
				datachannel.plugin.Kill()
				return nil
			case <-datachannel.plugin.Done():
				logger.Infof("datachannel's sole plugin is closed")
				return nil
			case agentMessage := <-datachannel.outputChan:
				// Push message to connection channel output
				datachannel.conn.Send(agentMessage)
			}
		}
	})

	return datachannel, nil
}

func (d *DataChannel) flushAllOutputChannelMessages() {
	for {
		select {
		case agentMessage := <-d.outputChan:
			// Push message to connection channel output
			d.conn.Send(agentMessage)
		case <-time.After(500 * time.Millisecond):
			return
		}
	}
}

func (d *DataChannel) Close(reason error) {
	if d.tmb.Alive() {
		d.logger.Infof("Datachannel closing because: %s", reason)

		d.tmb.Kill(reason)

		select {
		case <-d.tmb.Dead():
		case <-time.After(closeTimeout):
			d.logger.Infof("Timed out after %s waiting for datachannel to close", closeTimeout.String())
		}
	} else {
		d.logger.Infof("Close was called while in a dying state")
	}
}

// Wraps and sends the payload
func (d *DataChannel) send(messageType am.MessageType, messagePayload interface{}) {
	messageBytes, _ := json.Marshal(messagePayload)
	agentMessage := am.AgentMessage{
		ChannelId:      d.id,
		MessageType:    messageType,
		SchemaVersion:  am.CurrentVersion,
		MessagePayload: messageBytes,
	}

	d.outputChan <- agentMessage
}

func (d *DataChannel) sendMrtap(mrtapMessage *message.MrtapMessage, action string, payload []byte) error {
	// Build and send response
	if ackMessage, err := d.mrtap.BuildAck(mrtapMessage, action, payload); err != nil {
		rerr := fmt.Errorf("could not build response message: %s", err)
		d.logger.Error(rerr)
		return rerr
	} else {
		// TODO: CWC-2183; we still send a legacy message to accommodate older daemons. Newer ones can handle either
		d.send(am.MrtapLegacy, ackMessage)
		return nil
	}
}

func (d *DataChannel) sendError(errType bzerror.ErrorType, err error, hash string) {
	d.logger.Error(err)

	errMsg := bzerror.ErrorMessage{
		SchemaVersion: bzerror.CurrentVersion,
		Timestamp:     time.Now().Unix(),
		Type:          errType,
		Message:       err.Error(),
		HPointer:      hash,
	}
	d.send(am.Error, errMsg)
}

func (d *DataChannel) Receive(agentMessage am.AgentMessage) {
	// only push to input channel if we're alive (aka not in the process of dying or already dead)
	if d.tmb.Alive() {
		d.inputChan <- agentMessage
	}
}

func (d *DataChannel) processInput(agentMessage am.AgentMessage) {
	d.logger.Infof("received message type: %s", agentMessage.MessageType)

	switch am.MessageType(agentMessage.MessageType) {
	case am.Mrtap:
		var mrtapMessage message.MrtapMessage
		if err := json.Unmarshal(agentMessage.MessagePayload, &mrtapMessage); err != nil {
			d.sendError(bzerror.ComponentProcessingError, fmt.Errorf("malformed MrTAP message: %s", err), "")
		} else {
			d.handleMrtapMessage(&mrtapMessage)
		}
	default:
		rerr := fmt.Errorf("unhandled message type: %s", agentMessage.MessageType)
		d.sendError(bzerror.ComponentProcessingError, rerr, "")
	}
}

func (d *DataChannel) handleMrtapMessage(mrtapMessage *message.MrtapMessage) error {
	if err := d.mrtap.Validate(mrtapMessage); err != nil {
		rerr := fmt.Errorf("invalid MrTAP message: %s", err)
		// we send a legacy validation error to accommodate older daemons; new ones can handle either
		d.sendError(bzerror.MrtapLegacyValidationError, rerr, mrtapMessage.Hash())
		return rerr
	}

	switch mrtapMessage.Type {
	case message.Syn:
		synPayload := mrtapMessage.Payload.(message.SynPayload)

		if d.plugin == nil {
			// Grab user's action
			if parsedAction := strings.Split(synPayload.Action, "/"); len(parsedAction) <= 1 {
				rerr := fmt.Errorf("malformed action: %s", synPayload.Action)
				d.sendError(bzerror.ComponentProcessingError, rerr, mrtapMessage.Hash())
				return rerr
			} else {
				// Start plugin based on action
				actionPrefix := parsedAction[0]
				if err := d.startPlugin(bzplugin.PluginName(actionPrefix), synPayload.Action, synPayload.ActionPayload, synPayload.SchemaVersion); err != nil {
					d.sendError(bzerror.ComponentStartupError, err, mrtapMessage.Hash())
					return err
				}
			}
		}

		d.sendMrtap(mrtapMessage, "", []byte{}) // empty payload
	case message.Data:
		dataPayload := mrtapMessage.Payload.(message.DataPayload)

		if d.plugin == nil { // Can't process data message if no plugin created
			rerr := fmt.Errorf("plugin does not exist")
			d.sendError(bzerror.ComponentProcessingError, rerr, mrtapMessage.Hash())
			return rerr
		}

		// optionally clean our action payload to compensate for old bug that added extra quotes
		actionPayload := dataPayload.ActionPayload
		if !d.payloadClean {
			if cleaned, err := cleanPayload(actionPayload); err != nil {
				return fmt.Errorf("failed to clean payload: %s", err)
			} else {
				actionPayload = cleaned
			}
		}

		// Send message to plugin and catch response action payload
		if returnPayload, err := d.plugin.Receive(dataPayload.Action, actionPayload); err == nil {
			// Build and send response
			d.sendMrtap(mrtapMessage, dataPayload.Action, returnPayload)
		} else {
			rerr := fmt.Errorf("plugin error processing MrTAP message: %s", err)
			d.sendError(bzerror.ComponentProcessingError, rerr, mrtapMessage.Hash())
		}
	default:
		rerr := fmt.Errorf("invalid MrTAP Payload")
		d.sendError(bzerror.ComponentProcessingError, rerr, mrtapMessage.Hash())
		return rerr
	}
	return nil
}

func (d *DataChannel) startPlugin(pluginName bzplugin.PluginName, action string, payload []byte, version string) error {
	d.logger.Infof("Starting %v plugin", pluginName)

	// create channel and listener and pass it to the new plugin
	// TODO: get rid of this and just have an output() in the plugin we can listen to above
	streamOutputChan := make(chan smsg.StreamMessage, 30)
	go func() {
		for {
			select {
			case <-d.tmb.Dying():
				return
			case streamMessage := <-streamOutputChan:
				d.logger.Infof("Sending %s - %s - %t stream message", streamMessage.Action, streamMessage.Type, streamMessage.More)
				d.send(am.Stream, streamMessage)
			}
		}
	}()

	subLogger := d.logger.GetPluginLogger(pluginName)

	var err error
	switch pluginName {
	case bzplugin.Kube:
		d.plugin, err = kube.New(subLogger, streamOutputChan, action, payload)
	case bzplugin.Shell:
		d.plugin, err = shell.New(subLogger, streamOutputChan, action, payload)
	case bzplugin.Ssh:
		d.plugin, err = ssh.New(subLogger, streamOutputChan, action, payload)
	case bzplugin.Web:
		d.plugin, err = web.New(subLogger, streamOutputChan, action, payload)
	case bzplugin.Db:
		d.plugin, err = db.New(subLogger, streamOutputChan, action, payload)
	default:
		return fmt.Errorf("unrecognized plugin name %s", string(pluginName))
	}

	if err != nil {
		rerr := fmt.Errorf("failed to start %s plugin with %s action: %s", pluginName, action, err)
		d.logger.Error(rerr)
		return rerr
	} else {
		if c, err := semver.NewConstraint(">= 2.0"); err != nil {
			return fmt.Errorf("unable to create versioning constraint")
		} else if v, err := semver.NewVersion(version); err != nil {
			return fmt.Errorf("unable to parse version")
		} else {
			d.payloadClean = c.Check(v)
		}

		d.logger.Infof("%s plugin started!", pluginName)
		return nil
	}
}

func cleanPayload(payload []byte) ([]byte, error) {
	// TODO: CWC-1819: remove once all daemon's are updated
	if len(payload) > 0 {
		payload = payload[1 : len(payload)-1]
	}

	// Json unmarshalling encodes bytes in base64
	if payloadSafe, err := base64.StdEncoding.DecodeString(string(payload)); err != nil {
		return []byte{}, fmt.Errorf("error decoding actionPayload: %s", err)
	} else {
		return payloadSafe, nil
	}
}
