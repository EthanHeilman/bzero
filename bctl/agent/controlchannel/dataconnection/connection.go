/*
This package covers the Agent's data connection, used for communicating on behalf of
datachannels. It plays the role of our connection manager, implementing any
connection-specific logic to the agent's datachannel. For example, we do not attempt
to reconnect on disconnect.

Layers of the connection architecture:
1. Transporter
2. Messenger
3. Connection Manager <- this is us

See bzerolib/connection/connection.go for more information
*/
package dataconnection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
	"bastionzero.com/bctl/v1/bctl/agent/datachannel"
	"bastionzero.com/bctl/v1/bctl/agent/mrtap"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger"
	"bastionzero.com/bctl/v1/bzerolib/keypair"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/cenkalti/backoff"
	"gopkg.in/tomb.v2"
)

// This is a variable to control test duration
var maxBackoffInterval = 5 * time.Second // At most 3 seconds in between requests

const (
	MaximumReconnectWaitTime = 5 * time.Minute
	agentHubEndpoint         = "hub/agent/v2"

	// Websocket methods
	requestBastionToAgentV1 = "RequestBastionToAgentV1"
	openDataChannel         = "OpenDataChannel"
	closeDataChannel        = "CloseDataChannel"
	daemonConnected         = "DaemonConnected"
	daemonDisconnected      = "DaemonDisconnected"
	closeConnection         = "CloseConnection"
)

type DataConnection struct {
	tmb          tomb.Tomb
	logger       *logger.Logger
	ready        bool
	connectionId string

	// This is our underlying connection
	client messenger.Messenger

	// A connection broker, allows us to narrowcast to one subscribed datachannel
	broker *broker.Broker

	// Buffered channel to keep track of outbound messages
	sendQueue chan *am.AgentMessage

	// config values needed for MrTAP when creating datachannels
	mrtapConfig mrtap.MrtapConfig

	// provider of agent identity token and message signer for authenticating messages to the backend
	agentIdentityProvider agentidentity.IAgentIdentityProvider
	privateKey            *keypair.PrivateKey

	// Channel indicating when the DaemonConnected control message is sent in the websocket
	daemonReadyChan chan bool
}

func New(
	logger *logger.Logger,
	connUrl string,
	connectionId string,
	mrtapConfig mrtap.MrtapConfig,
	agentIdentityProvider agentidentity.IAgentIdentityProvider,
	privateKey *keypair.PrivateKey,
	params url.Values,
	headers http.Header,
	client messenger.Messenger,
) (*DataConnection, error) {

	// Check if the connection url is a validly formatted url
	connectionUrl, err := url.ParseRequestURI(connUrl)
	if err != nil {
		return nil, err
	}
	connectionUrl.Path = path.Join(connectionUrl.Path, agentHubEndpoint)

	conn := DataConnection{
		logger:                logger,
		connectionId:          connectionId,
		client:                client,
		broker:                broker.New(),
		sendQueue:             make(chan *am.AgentMessage, 50),
		mrtapConfig:           mrtapConfig,
		agentIdentityProvider: agentIdentityProvider,
		privateKey:            privateKey,
		daemonReadyChan:       make(chan bool),
	}

	if err := conn.connect(connectionUrl, headers, params); err != nil {
		return nil, err
	}

	go conn.receive()

	conn.tmb.Go(func() error {
		conn.logger.Infof("Connection has started")
		defer conn.logger.Infof("Connection has stopped")

		for {
			select {
			case <-conn.tmb.Dying():
				conn.ready = false

				// Close any listening datachannels
				conn.broker.Close(fmt.Errorf("connection closed"))

				// Sends a message to the daemon that we are closing the data connection
				// websocket so that the daemon can also disconnect from the websocket
				cdwMessaged := CloseDaemonWebsocketMessage{
					Reason: conn.Err().Error(),
				}
				messagePayloadBytes, err := json.Marshal(cdwMessaged)
				if err != nil {
					conn.logger.Errorf("Failed to marshal close daemon websocket message %s", err)
				} else {
					cdwMessage := am.AgentMessage{
						MessageType:    am.CloseDaemonWebsocket,
						MessagePayload: messagePayloadBytes,
						SchemaVersion:  am.CurrentVersion,
						ChannelId:      "-1", // Channel Id does not since this applies to all datachannels
					}

					conn.Send(cdwMessage)
				}

				// close the send queue and send all remaining messages before
				// closing the websocket
				conn.sendRemainingMessages()

				// Close the underlying connection
				conn.client.Close(conn.tmb.Err())

				return nil
			case <-conn.client.Done():
				conn.ready = false
				logger.Infof("Lost connection to BastionZero, reconnecting...")
				if err := conn.connect(connectionUrl, headers, params); err != nil {
					logger.Errorf("failed to reconnect to BastionZero: %s", err)
					return err
				}
			case message := <-conn.sendQueue:
				if err := conn.client.Send(*message); err != nil {
					conn.logger.Errorf("failed to send message: %s", err)
				} else {
					conn.logger.Infof("Sent %s message", message.MessageType)
				}
			}
		}
	})

	return &conn, nil
}

func (d *DataConnection) receive() {
	for {
		select {
		case <-d.tmb.Dead():
			return
		case message := <-d.client.Inbound():
			switch message.Target {
			case closeConnection:
				var rerr error
				var cawMessage CloseAgentWebsocketMessage
				if err := json.Unmarshal(message.Arguments[0], &cawMessage); err != nil {
					rerr = fmt.Errorf("error unmarshalling close agent websocket message. Error: %s", err)
				} else {
					rerr = fmt.Errorf("the daemon terminated the connection with reason %s", cawMessage)
				}
				d.tmb.Kill(rerr)
			case daemonConnected:
				d.logger.Info("daemon is connected")
				d.daemonReadyChan <- true
			case daemonDisconnected:
				reconnectTimeout := 60 * time.Second
				d.logger.Infof("daemon disconnected...waiting %s for daemon to reconnect", reconnectTimeout)
				go d.waitForDaemonToConnect(reconnectTimeout)
			case openDataChannel:
				var odMessage OpenDataChannelMessage
				err := json.Unmarshal(message.Arguments[0], &odMessage)
				if err != nil {
					d.logger.Errorf("error unmarshalling open data channel message: %s. Payload: %v", err, message.Arguments)
				}
				if err := d.openDataChannel(odMessage); err != nil {
					d.logger.Errorf("error handling open data channel control message: %s", err)
				}
			case closeDataChannel:
				var cdMessage CloseDataChannelMessage
				err := json.Unmarshal(message.Arguments[0], &cdMessage)
				if err != nil {
					d.logger.Errorf("error unmarshalling close data channel message: %s. Payload: %v", err, message.Arguments)
				}
				if err := d.closeDataChannel(cdMessage); err != nil {
					d.logger.Errorf("error handling close data channel control message: %s", err)
				}
			case requestBastionToAgentV1:
				// Assume that the invocation contains a single AgentMessage argument
				if len(message.Arguments) != 1 {
					d.logger.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
				}

				var agentMessage am.AgentMessage
				if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
					d.logger.Errorf("error unmarshalling %s message: %s", message.Target, err)
				}

				// forward the message to the datachannel using the broker
				if err := d.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
					d.logger.Errorf("failed to forward agent message to data channel: %s", err)
				}
			default:
				d.logger.Errorf("Unhandled method target: %s", message.Target)
			}
		}
	}
}

func (d *DataConnection) openDataChannel(odMessage OpenDataChannelMessage) error {
	dcId := odMessage.DataChannelId
	d.logger.Infof("got new open data channel control message for id: %s", dcId)

	subLogger := d.logger.GetDatachannelLogger(dcId)
	ksSubLogger := subLogger.GetComponentLogger("mrtap")

	if mt, err := mrtap.New(ksSubLogger, d.mrtapConfig); err != nil {
		return err
	} else {
		_, err := datachannel.New(&d.tmb, subLogger, d, mt, dcId, odMessage.Syn)
		return err
	}
}

func (d *DataConnection) closeDataChannel(cdMessage CloseDataChannelMessage) error {
	dcId := cdMessage.DataChannelId
	d.logger.Infof("got new close data channel control message for %s", dcId)

	if ok := d.broker.CloseChannel(dcId, fmt.Errorf("received close data channel control message from daemon")); !ok {
		return fmt.Errorf("agent connection does not have a datachannel with id: %s", dcId)
	}

	return nil
}

func (d *DataConnection) Send(agentMessage am.AgentMessage) {
	d.sendQueue <- &agentMessage
}

// add channel to channels dictionary for forwarding incoming messages
func (d *DataConnection) Subscribe(id string, channel broker.IChannel) {
	d.broker.Subscribe(id, channel)
}

func (d *DataConnection) Ready() bool {
	return d.ready
}

func (d *DataConnection) Done() <-chan struct{} {
	return d.tmb.Dead()
}

func (d *DataConnection) Err() error {
	return d.tmb.Err()
}

func (d *DataConnection) Close(reason error, timeout time.Duration) {
	if d.tmb.Alive() {
		d.logger.Infof("Connection closing because: %s", reason)
		d.tmb.Kill(reason)

		select {
		case <-d.tmb.Dead():
		case <-time.After(timeout):
			d.logger.Infof("Timed out after %s waiting for connection to close", timeout.String())
		}
	} else {
		d.logger.Infof("Close was called while in a dying state")
	}
}

func (d *DataConnection) sendRemainingMessages() {
	// close the send queue and send all remaining messages
	d.logger.Infof("sending remaining %d message(s) in send queue before closing websocket", len(d.sendQueue))
	sendQueueLength := len(d.sendQueue)
	for i := 0; i < sendQueueLength; i++ {
		message := <-d.sendQueue
		if err := d.client.Send(*message); err != nil {
			d.logger.Errorf("failed to send message: %s", err)
		} else {
			d.logger.Infof("Sent %s message", message.MessageType)
		}
	}

	if len(d.sendQueue) > 0 {
		d.logger.Errorf("more messages were added to the send queue after the connection was in dying state")
	}
}

func (d *DataConnection) connect(connUrl *url.URL, headers http.Header, params url.Values) error {
	// Make a context and tie it in with our tomb and then send it everywhere
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-d.tmb.Dying():
			cancel()
		}
	}()

	// Setup our exponential backoff parameters
	backoffParams := backoff.NewExponentialBackOff()
	backoffParams.MaxElapsedTime = MaximumReconnectWaitTime
	backoffParams.MaxInterval = maxBackoffInterval

	ticker := backoff.NewTicker(backoffParams)

	for {
		select {
		case <-d.tmb.Dying():
			return nil
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			agentIdentityToken, err := d.agentIdentityProvider.GetToken(ctx)
			if err != nil {
				d.logger.Errorf("Retrying in %s because failed to get agent identity token: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			openAgentWebsocketMessage := OpenAgentWebsocketMessage{
				BackendAgentMessage: am.BackendAgentMessage{
					MessageType: am.OpenAgentWebsocket,
					Timestamp:   time.Now().Unix(),
				},
				ConnectionId: d.connectionId,
			}

			openAgentWebsocketPayload, err := json.Marshal(openAgentWebsocketMessage)
			if err != nil {
				return fmt.Errorf("error marshalling openAgentWebsocket message: %w", err)
			}

			// Sign the message
			sig := d.privateKey.Sign(openAgentWebsocketPayload)

			// Add our AgentIdentityToken as Bearer Authorization header
			headers["Authorization"] = []string{fmt.Sprintf("Bearer %s", agentIdentityToken)}

			// Add message + signature as query params
			params["message"] = []string{base64.StdEncoding.EncodeToString(openAgentWebsocketPayload)}
			params["signature"] = []string{sig}

			if err := d.client.Connect(ctx, connUrl.String(), headers, params, targetSelectHandler); err != nil {
				d.logger.Errorf("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			d.logger.Infof("Successfully connected to %s", connUrl)
			d.ready = true

			go d.waitForDaemonToConnect(60 * time.Second)
			return nil
		}
	}
}

// wait for daemon connect message or timeout and close the connection
func (d *DataConnection) waitForDaemonToConnect(timeout time.Duration) {
	select {
	case <-d.tmb.Dying():
		break
	case <-d.daemonReadyChan:
		break
	case <-time.After(timeout):
		d.Close(fmt.Errorf("timed out waiting for daemon to connect after %s", timeout), 10*time.Second)
	}
}

// agent's data channel function to select signalR hub method based on agent message type
func targetSelectHandler(agentMessage am.AgentMessage) (string, error) {
	switch am.MessageType(agentMessage.MessageType) {
	case am.CloseDaemonWebsocket:
		return "CloseDaemonWebsocketV1", nil
	case am.Mrtap, am.MrtapLegacy, am.Stream, am.Error:
		return "ResponseAgentToBastionV1", nil
	default:
		return "", fmt.Errorf("unable to determine SignalR endpoint for message type: %s", agentMessage.MessageType)
	}
}

func (d *DataConnection) NumDataChannels() int {
	return d.broker.NumChannels()
}
