package dataconnection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/cenkalti/backoff"
	"gopkg.in/tomb.v2"
)

type AgentConnectedMessage struct {
	ConnectionId string `json:"connectionId"`
}

type ConnectionType string

const (
	Shell ConnectionType = "SHELL"
	Ssh   ConnectionType = "SSH"
	Kube  ConnectionType = "CLUSTER"
	Db    ConnectionType = "DB"
	Web   ConnectionType = "WEB"
)

var maxBackoffInterval = 15 * time.Minute

const (
	daemonHubEndpoint = "hub/daemon"

	// SignalR methods that we need to know for processing
	agentConnected    = "AgentConnected"
	agentDisconnected = "CloseConnection"

	// How long the daemon waits for the agent to connect
	agentConnectedTimeout = 30 * time.Second

	MaximumReconnectWaitTime = 1 * time.Hour
)

type DataConnection struct {
	tmb    tomb.Tomb
	logger *logger.Logger
	ready  bool

	// This is our underlying connection
	client messenger.Messenger

	// A connection broker, allows us to narrowcast to one subscribed datachannel
	broker *broker.Broker

	// Buffered channel to keep track of outbound messages
	sendQueue chan *am.AgentMessage

	// Agent Ready Channel indicates when the agent has connected to the
	// corresponding connection. This is only used for daemon datachannel
	// connections.
	agentReadyChan chan bool
	agentReady     bool
}

func New(
	logger *logger.Logger,
	connUrl string,
	params url.Values,
	headers http.Header,
	client messenger.Messenger,
) (connection.Connection, error) {

	// Check if the connection url is a validly formatted url
	connectionUrl, err := url.ParseRequestURI(connUrl)
	if err != nil {
		return nil, err
	}
	connectionUrl.Path = path.Join(connectionUrl.Path, daemonHubEndpoint)

	conn := DataConnection{
		logger:         logger,
		client:         client,
		broker:         broker.New(),
		sendQueue:      make(chan *am.AgentMessage, 50),
		agentReadyChan: make(chan bool, 1),
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
				if !conn.ready {
					// Wait for the agent to connect before sending any messages
					// from the output queue. We only do this on the initial connection
					// because daemon will only try to reconnect if the agent is
					// still connected
					conn.waitForAgentReady()
				}
				if err := conn.client.Send(*message); err != nil {
					conn.logger.Errorf("failed to send message: %s", err)
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
			if err := d.processInbound(*message); err != nil {
				d.logger.Error(err)
			}
		}
	}
}

// Returns error on connection closed
func (d *DataConnection) processInbound(message signalr.SignalRMessage) error {
	switch message.Target {
	case agentDisconnected:
		rerr := fmt.Errorf("the bzero agent terminated the connection, not retrying")
		d.tmb.Kill(rerr)
		return rerr
	case agentConnected:
		// Signal the agentReady channel when we receive a message
		// from the connection node that the agent is connected
		var agentConnectedMessage AgentConnectedMessage
		if err := json.Unmarshal(message.Arguments[0], &agentConnectedMessage); err != nil {
			return fmt.Errorf("error unmarshalling agent connected message. Error: %s", err)
		}

		d.logger.Infof("Agent is connected and ready to receive for connection: %s", agentConnectedMessage.ConnectionId)

		d.agentReady = true
		d.ready = true
		d.agentReadyChan <- true
	default:
		// Otherwise assume that the invocation contains a single AgentMessage argument
		if len(message.Arguments) != 1 {
			return fmt.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
		}

		var agentMessage am.AgentMessage
		if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
			return fmt.Errorf("error unmarshalling %s message: %w", message.Target, err)
		}

		if err := d.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
			return fmt.Errorf("failed to forward agent message to datachannel: %w", err)
		}
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

func (d *DataConnection) connect(connUrl *url.URL, headers http.Header, params url.Values) error {
	d.logger.Infof("Establishing connection with %s", connUrl.String())

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

			if err := d.client.Connect(ctx, connUrl.String(), headers, params, targetSelectHandler); err != nil {
				d.logger.Infof("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			// If we're reconnecting, then we don't need to wait for this message
			if d.agentReady {
				d.ready = true
			}

			d.logger.Infof("Successfully connected to %s", connUrl)
			return nil
		}
	}
}

func (d *DataConnection) waitForAgentReady() {
	select {
	case <-d.tmb.Dying():
		return
	case <-d.agentReadyChan:
		d.agentReady = true
		d.ready = true
	case <-time.After(agentConnectedTimeout):
		d.tmb.Kill(fmt.Errorf("timed out waiting for agent to connect"))
	}
}

// daemon's data channel function to select signalR hub method based on agent message type
func targetSelectHandler(agentMessage am.AgentMessage) (string, error) {
	switch am.MessageType(agentMessage.MessageType) {
	case am.Keysplitting:
		return "RequestDaemonToBastionV1", nil
	case am.OpenDataChannel:
		return "OpenDataChannelDaemonToBastionV1", nil
	case am.CloseDataChannel:
		return "CloseDataChannelDaemonToBastionV1", nil
	default:
		return "", fmt.Errorf("unhandled message type: %s", agentMessage.MessageType)
	}
}
