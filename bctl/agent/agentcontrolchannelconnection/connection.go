package agentcontrolchannelconnection

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

	"bastionzero.com/bctl/v1/bctl/agent/agentcontrolchannelconnection/challenge"
	"bastionzero.com/bctl/v1/bctl/agent/vault"
	"bastionzero.com/bctl/v1/bzerolib/connection"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	controlHubEndpoint = "/api/v1/hub/control"
)

type AgentControlChannelConnection struct {
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
	transporter transporter.Transporter,
) (connection.Connection, error) {
	logger.Infof("CREATING A NEW CONTROL CHANNEL CONNECTION")
	srLogger := logger.GetComponentLogger("SignalR")
	client := signalr.New(srLogger, transporter)

	conn := AgentControlChannelConnection{
		logger:    logger,
		client:    client,
		broker:    broker.New(),
		sendQueue: make(chan *am.AgentMessage, 50),
	}

	if err := conn.connect(connUrl, headers, params); err != nil {
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
				if err := conn.connect(connUrl, headers, params); err != nil {
					logger.Errorf("failed to connect to BastionZero: %s", err)
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

func (a *AgentControlChannelConnection) receive() {
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

func (a *AgentControlChannelConnection) Send(agentMessage am.AgentMessage) {
	a.sendQueue <- &agentMessage
}

// add channel to channels dictionary for forwarding incoming messages
func (a *AgentControlChannelConnection) Subscribe(id string, channel broker.IChannel) {
	a.broker.Subscribe(id, channel)
}

func (a *AgentControlChannelConnection) Ready() bool {
	return a.ready
}

func (a *AgentControlChannelConnection) Done() <-chan struct{} {
	return a.tmb.Dead()
}

func (a *AgentControlChannelConnection) Err() error {
	return a.tmb.Err()
}

func (a *AgentControlChannelConnection) Close(reason error) {
	if a.tmb.Alive() {
		a.tmb.Kill(reason)
		a.tmb.Wait()
	}
}

func (a *AgentControlChannelConnection) connect(connUrl string, headers http.Header, params url.Values) error {
	// Check if the connection url is a validly formatted url
	connectionUrl, err := url.Parse(connUrl)
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

			// First get the config from the vault
			config, err := vault.LoadVault()
			if err != nil {
				return fmt.Errorf("failed to retrieve agent vault: %s", err)
			}

			if solvedChallenge, err := challenge.Get(a.logger, connUrl, params["target_id"][0], params["version"][0], config.Data.PrivateKey, ctx); err != nil {
				a.logger.Debugf("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			} else {
				params["solved_challenge"] = []string{solvedChallenge}
			}

			// And sign our agent version
			if signedAgentVersion, err := challenge.Solve(params["version"][0], config.Data.PrivateKey); err != nil {
				return fmt.Errorf("error signing agent version: %s", err)
			} else {
				params["signed_agent_version"] = []string{signedAgentVersion}
			}

			if err := a.client.Connect(ctx, connectionUrl.String(), headers, params, targetSelectHandler); err != nil {
				a.logger.Infof("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
				continue
			} else {
				a.logger.Infof("Successfully connected!")
				a.ready = true
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
