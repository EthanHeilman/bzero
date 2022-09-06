package datachannelconnection

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
)

type DataChannelConnection struct {
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

	conn := DataChannelConnection{
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

func (a *DataChannelConnection) receive() {
	for {
		select {
		case <-a.tmb.Dead():
			return
		case message := <-a.client.Inbound():

			// Otherwise assume that the invocation contains a single AgentMessage argument
			if len(message.Arguments) != 1 {
				a.logger.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
			}

			var agentMessage am.AgentMessage
			if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
				a.logger.Errorf("error unmarshalling %s message: %s", message.Target, err)
			}

			if err := a.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
				a.logger.Errorf("failed to forward agent message to datachannel: %s", err)
			}
		}
	}
}

func (a *DataChannelConnection) Send(agentMessage am.AgentMessage) {
	a.sendQueue <- &agentMessage
}

// add channel to channels dictionary for forwarding incoming messages
func (a *DataChannelConnection) Subscribe(id string, channel broker.IChannel) {
	a.broker.Subscribe(id, channel)
}

func (a *DataChannelConnection) Ready() bool {
	return a.ready
}

func (a *DataChannelConnection) Done() <-chan struct{} {
	return a.tmb.Dead()
}

func (a *DataChannelConnection) Err() error {
	return a.tmb.Err()
}

func (a *DataChannelConnection) Close(reason error) {
	if a.tmb.Alive() {
		a.tmb.Kill(reason)
		a.tmb.Wait()
	}
}

func (a *DataChannelConnection) connect(connUrl *url.URL, headers http.Header, params url.Values) error {
	// Make a context and tie it in with our tomb and then send it everywhere
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-a.tmb.Dying():
			cancel()
		}
	}()

	// Setup our exponential backoff parameters
	backoffParams := backoff.NewExponentialBackOff()
	backoffParams.MaxElapsedTime = time.Hour * 72 // Wait in total at most 72 hours
	backoffParams.MaxInterval = time.Minute * 15  // At most 15 minutes in between requests

	ticker := backoff.NewTicker(backoffParams)

	for {
		select {
		case <-a.tmb.Dying():
			return nil
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			if err := a.client.Connect(ctx, connUrl.String(), headers, params, targetSelectHandler); err != nil {
				a.logger.Infof("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			a.logger.Infof("Successfully connected to %s", connUrl)
			a.ready = true
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
