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
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/cenkalti/backoff"
	"gopkg.in/tomb.v2"
)

const (
	agentHubEndpoint = "hub/agent"

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
	connectionUrl.Path = path.Join(connectionUrl.Path, agentHubEndpoint)

	conn := DataConnection{
		logger:    logger,
		client:    client,
		broker:    broker.New(),
		sendQueue: make(chan *am.AgentMessage, 50),
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
				logger.Infof("Connection with BastionZero closed and we're not retrying")
				return fmt.Errorf("underlying connection closed")
			case message := <-conn.sendQueue:
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

			// Otherwise assume that the invocation contains a single AgentMessage argument
			if len(message.Arguments) != 1 {
				d.logger.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
			}

			var agentMessage am.AgentMessage
			if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
				d.logger.Errorf("error unmarshalling %s message: %s", message.Target, err)
			}

			if err := d.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
				d.logger.Errorf("failed to forward agent message to datachannel: %s", err)
			}
		}
	}
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
	backoffParams.MaxInterval = time.Minute * 15 // At most 15 minutes in between requests

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

			d.logger.Infof("Successfully connected to %s", connUrl)
			d.ready = true
			return nil
		}
	}
}

// agent's data channel function to select signalR hub method based on agent message type
func targetSelectHandler(agentMessage am.AgentMessage) (string, error) {
	switch am.MessageType(agentMessage.MessageType) {
	case am.CloseDaemonWebsocket:
		return "CloseDaemonWebsocketV1", nil
	case am.Keysplitting, am.Stream, am.Error:
		return "ResponseAgentToBastionV1", nil
	default:
		return "", fmt.Errorf("unable to determine SignalR endpoint for message type: %s", agentMessage.MessageType)
	}
}
