package controlchannelconnection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/cenkalti/backoff"
	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannelconnection/challenge"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	controlHubEndpoint       = "/api/v1/hub/control"
	waitForCloseTimeout      = 10 * time.Second
	MaximumReconnectWaitTime = 1 * time.Hour
)

type ControlChannelConnection struct {
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
	privateKey string,
	params url.Values,
	headers http.Header,
	client messenger.Messenger,
) (connection.Connection, error) {

	conn := ControlChannelConnection{
		logger:    logger,
		client:    client,
		broker:    broker.New(),
		sendQueue: make(chan *am.AgentMessage, 50),
	}

	if err := conn.connect(connUrl, headers, params, privateKey); err != nil {
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
				if err := conn.connect(connUrl, headers, params, privateKey); err != nil {
					logger.Errorf("failed to reconnect to BastionZero: %s", err)
					return err
				}
			case message := <-conn.sendQueue:
				if err := conn.client.Send(*message); err != nil {
					conn.logger.Errorf("failed to send message: %s", err)
				}
			}
		}
	})

	return &conn, nil
}

func (c *ControlChannelConnection) receive() {
	for {
		select {
		case <-c.tmb.Dead():
			return
		case message := <-c.client.Inbound():

			// Otherwise assume that the invocation contains a single AgentMessage argument
			if len(message.Arguments) != 1 {
				c.logger.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
			}

			var agentMessage am.AgentMessage
			if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
				c.logger.Errorf("error unmarshalling %s message: %s", message.Target, err)
			}

			if err := c.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
				c.logger.Errorf("failed to forward agent message to datachannel: %s", err)
			}
		}
	}
}

func (c *ControlChannelConnection) Send(agentMessage am.AgentMessage) {
	c.sendQueue <- &agentMessage
}

// add channel to channels dictionary for forwarding incoming messages
func (c *ControlChannelConnection) Subscribe(id string, channel broker.IChannel) {
	c.broker.Subscribe(id, channel)
}

func (c *ControlChannelConnection) Ready() bool {
	return c.ready
}

func (c *ControlChannelConnection) Done() <-chan struct{} {
	return c.tmb.Dead()
}

func (c *ControlChannelConnection) Err() error {
	return c.tmb.Err()
}

func (c *ControlChannelConnection) Close(reason error) {
	if c.tmb.Alive() {
		c.tmb.Kill(reason)

		select {
		case <-c.tmb.Dead():
		case <-time.After(waitForCloseTimeout):
			c.logger.Info("Timed out waiting for connection to close")
		}
	}
}

func (c *ControlChannelConnection) connect(connUrl string, headers http.Header, params url.Values, privateKey string) error {
	// Check if the connection url is a validly formatted url
	connectionUrl, err := url.ParseRequestURI(connUrl)
	if err != nil {
		return err
	}
	// Build our endpoint which we'll use to connect to
	connectionUrl.Path = path.Join(connectionUrl.Path, controlHubEndpoint)

	// Make a context and tie it in with our tomb and then send it everywhere
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
		case <-c.tmb.Dying():
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
		case <-c.tmb.Dying():
			return nil
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			if solvedChallenge, err := challenge.Get(c.logger, connUrl, params["target_id"][0], params["version"][0], privateKey, ctx); err != nil {
				c.logger.Debugf("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			} else {
				params["solved_challenge"] = []string{solvedChallenge}
			}

			// And sign our agent version
			if signedAgentVersion, err := challenge.Solve(params["version"][0], privateKey); err != nil {
				return fmt.Errorf("error signing agent version: %s", err)
			} else {
				params["signed_agent_version"] = []string{signedAgentVersion}
			}

			if err := c.client.Connect(ctx, connectionUrl.String(), headers, params, targetSelectHandler); err != nil {
				c.logger.Infof("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			} else {
				c.logger.Infof("Successfully connected to %s", connUrl)
				c.ready = true
				return nil
			}
		}
	}
}

// agent's control channel function to select signalR hub method based on agent message type
func targetSelectHandler(agentMessage am.AgentMessage) (string, error) {
	switch am.MessageType(agentMessage.MessageType) {
	case am.HealthCheck:
		return "AliveCheckAgentToBastion", nil
	default:
		return "", fmt.Errorf("unsupported message type")
	}
}
