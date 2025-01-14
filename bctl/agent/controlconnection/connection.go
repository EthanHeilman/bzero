/*
This package covers the Agent's control connection, used for communicating on behalf of
control channels. It plays the role of our connection manager, implementing any
connection-specific logic to the agent's control channel. For example, we always retry
on disconnect.

Layers of the connection architecture:
1. Transporter
2. Messenger
3. Connection Manager <- this is us

See bzerolib/connection/connection.go for more information
*/
package controlconnection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/cenkalti/backoff/v4"
	"gopkg.in/tomb.v2"

	"bastionzero.com/agent/bastion/agentidentity"
	"bastionzero.com/bzerolib/connection"
	am "bastionzero.com/bzerolib/connection/agentmessage"
	"bastionzero.com/bzerolib/connection/broker"
	"bastionzero.com/bzerolib/connection/httpclient"
	"bastionzero.com/bzerolib/connection/messenger"
	"bastionzero.com/bzerolib/keypair"
	"bastionzero.com/bzerolib/logger"
)

var (
	maxBackoffInterval = 5 * time.Minute
	retryCount         = 0
)

const (
	waitForCloseTimeout      = 10 * time.Second
	MaximumReconnectWaitTime = 30 * time.Minute

	connectionServiceEndpoint = "/api/v2/connection-service/url" // bastion
	controlChannelEndpoint    = "/control-channel"               // connection-orchestrator
	controlChannelHubEndpoint = "hub/agent-control"              // connection-node
)

type ControlConnection struct {
	tmb    tomb.Tomb
	logger *logger.Logger
	ready  bool

	// This is our underlying connection
	client messenger.Messenger

	// A connection broker, allows us to narrowcast to one subscribed datachannel
	broker *broker.Broker

	// Provider of agent identity token and message signer for authenticating messages to the backend
	agentIdToken agentidentity.AgentIdentityToken

	// signing key
	privateKey *keypair.PrivateKey

	// Buffered channel to keep track of outbound messages
	sendQueue chan *am.AgentMessage
}

func New(
	logger *logger.Logger,
	bastionUrl string,
	privateKey *keypair.PrivateKey,
	params url.Values,
	headers http.Header,
	client messenger.Messenger,
	agentIdToken agentidentity.AgentIdentityToken,
) (connection.Connection, error) {

	conn := ControlConnection{
		logger:       logger,
		client:       client,
		broker:       broker.New(),
		agentIdToken: agentIdToken,
		privateKey:   privateKey,
		sendQueue:    make(chan *am.AgentMessage, 50),
	}

	go conn.receive()

	conn.tmb.Go(func() error {
		conn.logger.Infof("Connection has started")
		defer conn.logger.Infof("Connection has stopped")

		if err := conn.connect(bastionUrl, headers, params); err != nil {
			return err
		}

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
				if err := conn.connect(bastionUrl, headers, params); err != nil {
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

func (c *ControlConnection) receive() {
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

func (c *ControlConnection) Send(agentMessage am.AgentMessage) {
	c.sendQueue <- &agentMessage
}

// add channel to channels dictionary for forwarding incoming messages
func (c *ControlConnection) Subscribe(id string, channel broker.IChannel) {
	c.broker.Subscribe(id, channel)
}

func (c *ControlConnection) Ready() bool {
	return c.ready
}

func (c *ControlConnection) Done() <-chan struct{} {
	return c.tmb.Dead()
}

func (c *ControlConnection) Err() error {
	return c.tmb.Err()
}

func (c *ControlConnection) Close(reason error, timeout time.Duration) {
	if c.tmb.Alive() {
		c.logger.Infof("Connection closing because: %s", reason)

		c.tmb.Kill(reason)

		select {
		case <-c.tmb.Dead():
		case <-time.After(timeout):
			c.logger.Infof("Timed out after %s waiting for connection to close", timeout.String())
		}
	} else {
		c.logger.Infof("Close was called while in a dying state")
	}
}

func (c *ControlConnection) connect(bastionUrl string, headers http.Header, params url.Values) error {
	// Make sure bastionUrl is valid
	if _, err := url.ParseRequestURI(bastionUrl); err != nil {
		return err
	}

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
	backoffParams.MaxInterval = maxBackoffInterval

	retryCount = 0
	ticker := backoff.NewTicker(backoffParams)

	for {
		select {
		case <-c.tmb.Dying():
			return nil
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			retryCount++

			// get the connectionOrchestratorUrl from bastion
			connectionOrchestratorUrl, err := c.getConnectionServiceUrl(bastionUrl, ctx)
			if err != nil {
				c.logger.Infof("Retrying in %s because we failed to get connection service url from bastion: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			agentIdentityToken, err := c.agentIdToken.Get(ctx)
			if err != nil {
				c.logger.Infof("Retrying in %s because we failed to get agent identity token: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			getControlChannelResponse, err := c.getControlChannel(ctx, connectionOrchestratorUrl, agentIdentityToken)
			if err != nil {
				c.logger.Infof("Retrying in %s because we failed to get assigned a connection node from the orchestrator: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			}

			connectionUrl, err := url.Parse(getControlChannelResponse.ConnectionUrl)
			if err != nil {
				return err
			}
			connectionUrl.Path = path.Join(connectionUrl.Path, controlChannelHubEndpoint)

			message, sig, err := c.buildOpenControlChannelMessage(params["version"][0], getControlChannelResponse.ConnectionUrl, getControlChannelResponse.ControlChannelId)
			if err != nil {
				return fmt.Errorf("failed to build open control channel message %w", err)
			}

			headers["Authorization"] = []string{fmt.Sprintf("Bearer %s", agentIdentityToken)}
			params["message"] = []string{message}
			params["signature"] = []string{sig}

			if err := c.client.Connect(ctx, connectionUrl.String(), headers, params, targetSelectHandler); err != nil {
				c.logger.Infof("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			} else {
				c.logger.Infof("Successfully connected to %s", connectionUrl)
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
		return "Heartbeat", nil
	case am.ClusterUsers:
		return "ClusterUsers", nil
	default:
		return "", fmt.Errorf("unsupported message type")
	}
}

func (c *ControlConnection) getConnectionServiceUrl(serviceUrl string, ctx context.Context) (string, error) {
	// Build the http client and request
	options := httpclient.HTTPOptions{
		Endpoint: connectionServiceEndpoint,
	}

	client, err := httpclient.New(serviceUrl, options)
	if err != nil {
		return "", err
	}

	// make our request
	resp, err := client.Get(ctx)
	if err != nil {
		return "", err
	}

	// Decode and return response
	defer resp.Body.Close()
	responseDecoded := GetConnectionServiceResponse{}
	json.NewDecoder(resp.Body).Decode(&responseDecoded)
	return responseDecoded.ConnectionServiceUrl, nil
}

func (c *ControlConnection) getControlChannel(ctx context.Context, connUrl string, agentIdentityToken string) (*GetControlChannelResponse, error) {
	// Create a new GetControlChannel message
	getControlChannel := GetControlChannel{
		BackendAgentMessage: am.BackendAgentMessage{
			MessageType: am.GetControlChannel,
			Timestamp:   time.Now().Unix(),
		},
	}

	// Serialize the message
	getControlChannelPayload, err := json.Marshal(getControlChannel)
	if err != nil {
		return nil, fmt.Errorf("error marshalling getControlChannel message: %w", err)
	}

	// Sign the message
	sig := c.privateKey.Sign(getControlChannelPayload)

	// Build the http client and request
	options := httpclient.HTTPOptions{
		Endpoint: controlChannelEndpoint,
		Headers: http.Header{
			"Authorization": {fmt.Sprintf("Bearer %s", agentIdentityToken)},
		},
		Params: url.Values{
			"message":   {base64.StdEncoding.EncodeToString(getControlChannelPayload)},
			"signature": {sig},
		},
	}

	client, err := httpclient.New(connUrl, options)
	if err != nil {
		return nil, err
	}

	// Make request
	response, err := client.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("error making get request for control channel: %s", err)
	}

	// Decode and return response
	var responseDecoded GetControlChannelResponse
	json.NewDecoder(response.Body).Decode(&responseDecoded)
	return &responseDecoded, nil
}

func (c *ControlConnection) buildOpenControlChannelMessage(version string, connectionUrl string, controlChannelId string) (string, string, error) {
	// Create a new OpenControlChannel message
	openControlChannel := OpenControlChannel{
		Version:          version,
		ControlChannelId: controlChannelId,
		ConnectionUrl:    connectionUrl,
		BackendAgentMessage: am.BackendAgentMessage{
			MessageType: am.OpenControlChannel,
			Timestamp:   time.Now().Unix(),
		},
	}

	// Serialize the message
	openControlChannelPayload, err := json.Marshal(openControlChannel)
	if err != nil {
		return "", "", fmt.Errorf("error marshalling openControlChannel message: %w", err)
	}

	// Sign the message
	sig := c.privateKey.Sign(openControlChannelPayload)

	return base64.StdEncoding.EncodeToString(openControlChannelPayload), sig, nil
}
