package universalconnection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"gopkg.in/tomb.v2"

	"bastionzero.com/bctl/v1/bctl/agent/vault"
	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/challenge"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

type AgentConnectedMessage struct {
	ConnectionId string `json:"connectionId"`
}

// Connection Type enum
type ConnectionType string

const (
	Shell ConnectionType = "SHELL"
	Ssh   ConnectionType = "SSH"
	Kube  ConnectionType = "CLUSTER"
	Db    ConnectionType = "DB"
	Web   ConnectionType = "WEB"
)

type ChannelType int

const (
	AgentDataChannel ChannelType = iota
	AgentControlChannel
	DaemonDataChannel
)

const (
	// SignalR methods that we need to know for processing
	agentConnected    = "AgentConnected"
	agentDisconnected = "CloseConnection"

	// Hub endpoints
	daemonHubEndpoint  = "hub/daemon"
	agentHubEndpoint   = "hub/agent"
	controlHubEndpoint = "/api/v1/hub/control"

	// How long the daemon waits for the agent to connect
	agentConnectedTimeout = 30 * time.Second
)

type Messenger interface {
	Close(reason error)
	Done() <-chan struct{}
	Inbound() <-chan *signalr.SignalRMessage
	Connect(ctx context.Context, targetUrl string, headers http.Header, params url.Values, targetSelectHandler func(msg am.AgentMessage) (string, error)) error
	Send(message am.AgentMessage) error
}

type UniversalConnection struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	// This is our underlying connection where we send and receive messages
	client Messenger

	// A connection broker, allows us to narrowcast to one subscribed datachannel
	broker *broker.Broker

	// Buffered channel to keep track of outgoing messages
	sendQueue chan *am.AgentMessage

	// True when connection is ready to start sending output messages
	sendQueueReady bool

	// Type of connection being made
	channelType ChannelType

	// Agent Ready Channel indicates when the agent has connected to the
	// corresponding connection. This is only used for daemon datachannel
	// connections.
	agentReadyChan chan struct{}
}

func New(
	logger *logger.Logger,
	connectionUrl string,
	params url.Values,
	headers http.Header,
	autoReconnect bool,
	wtype ChannelType,
) (*UniversalConnection, error) {
	// Create our signalr object
	srLogger := logger.GetComponentLogger("SignalR")
	wsLogger := srLogger.GetComponentLogger("Websocket")
	client := signalr.New(srLogger, websocket.New(wsLogger))

	conn := UniversalConnection{
		logger:         logger,
		client:         client,
		broker:         broker.New(),
		sendQueue:      make(chan *am.AgentMessage, 50),
		channelType:    wtype,
		agentReadyChan: make(chan struct{}, 1),
	}

	if err := conn.connect(connectionUrl, headers, params); err != nil {
		logger.Error(err)
		conn.Close(fmt.Errorf("unable to connect to BastionZero"))
	}

	go conn.receive()

	conn.tmb.Go(func() error {
		conn.logger.Infof("Connection has started")
		defer conn.logger.Infof("Connection has stopped")

		for {
			select {
			case <-conn.tmb.Dying():
				conn.waitForRemainingMessages()
				conn.logger.Infof("Closing connection handlers")
				conn.client.Close(fmt.Errorf("connection closing"))
				return nil
			case <-conn.client.Done():
				conn.sendQueueReady = false

				if autoReconnect {
					logger.Infof("Lost connection to BastionZero, reconnecting...")

					if err := conn.connect(connectionUrl, headers, params); err != nil {
						logger.Errorf("failed to connect to BastionZero: %s", err)
						return err
					}
				} else {
					logger.Infof("Connection with BastionZero closed and we're not retrying")
					return nil
				}
			case message := <-conn.sendQueue:
				if !conn.sendQueueReady && conn.channelType == DaemonDataChannel {
					// If this is a daemon datachannel connection wait for the agent to
					// connect before sending any messages from the output queue
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

func (u *UniversalConnection) receive() {
	for {
		select {
		case <-u.tmb.Dead():
			return
		case message := <-u.client.Inbound():
			if err := u.processInbound(*message); err != nil {
				u.logger.Error(err)
			}
		}
	}
}

func (u *UniversalConnection) waitForRemainingMessages() {
	u.logger.Infof("Entering endgame for connection")
	exitTimer := time.Second
	absoluteTimeout := time.NewTimer(10 * time.Second)
	defer absoluteTimeout.Stop()

	for {
		select {
		case message := <-u.sendQueue:
			if !u.sendQueueReady && u.channelType == DaemonDataChannel {
				// If this is a daemon datachannel connection wait for the agent to
				// connect before sending any messages from the output queue
				u.waitForAgentReady()
			}

			if err := u.client.Send(*message); err != nil {
				u.logger.Errorf("failed to send message: %s", err)
			}
		case <-time.After(exitTimer):
			return
		case <-absoluteTimeout.C:
			u.logger.Errorf("timed out waiting for connection to receive all messages after close")
			return
		}
	}
}

func (u *UniversalConnection) Ready() bool {
	return u.sendQueueReady
}

func (u *UniversalConnection) Done() <-chan struct{} {
	return u.tmb.Dead()
}

func (u *UniversalConnection) Err() error {
	return u.tmb.Err()
}

// Close kills all datachannels subscribed to this connection, kills the
// tomb, disconnects the underlying connection, and waits for all
// goroutines tracked by the connection's tomb to finish running.
func (u *UniversalConnection) Close(reason error) {
	if !u.tmb.Alive() {
		return
	}
	u.close(reason)

	// It is not safe for tracked goroutines within the UniversalConnection struct
	// to call Wait() otherwise there is a deadlock when calling .Wait()
	u.tmb.Wait()
	u.logger.Infof("connection done")
}

// close kills all datachannels subscribed to this connection, kills the
// tomb, and disconnects the underlying universalconnection.
//
// Tracked goroutines that wish to close the connection must call this function,
// instead of the publicly exposed one, to ensure there is no deadlock.
func (u *UniversalConnection) close(reason error) {
	u.logger.Infof("connection closing because: %s", reason)

	// close all of our existing datachannels
	u.broker.Close(reason)
	// mark the tmb as dying so we ignore any errors that occur when closing the
	// connection
	u.tmb.Kill(reason)
	// close the underlying connection
	if u.client != nil {
		u.client.Close(reason)
	}
}

// add channel to channels dictionary for forwarding incoming messages
func (u *UniversalConnection) Subscribe(id string, channel broker.IChannel) {
	u.broker.Subscribe(id, channel)
}

// Returns error on connection closed
func (u *UniversalConnection) processInbound(message signalr.SignalRMessage) error {
	switch message.Target {
	case agentDisconnected:
		rerr := errors.New("the bzero agent terminated the connection")
		u.Close(rerr)
		return rerr
	case agentConnected:
		// Signal the agentReady channel when we receive a message
		// from the connection node that the agent is connected
		var agentConnectedMessage AgentConnectedMessage
		if err := json.Unmarshal(message.Arguments[0], &agentConnectedMessage); err != nil {
			return fmt.Errorf("error unmarshalling agent connected message. Error: %s", err)
		}

		u.logger.Infof("Agent is connected and ready to receive for connection: %s", agentConnectedMessage.ConnectionId)

		u.agentReadyChan <- struct{}{}
	default:
		// Otherwise assume that the invocation contains a single AgentMessage argument
		if len(message.Arguments) != 1 {
			return fmt.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
		}

		var agentMessage am.AgentMessage
		if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
			return fmt.Errorf("error unmarshalling %s message: %w", message.Target, err)
		}

		if err := u.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
			return fmt.Errorf("failed to forward agent message to datachannel: %w", err)
		}
	}
	return nil
}

// The go routine listening to this channel is only created on connection and is torn down
// when that connection is closed. Therefore, if someone wants to send a message this
// will block
func (u *UniversalConnection) Send(agentMessage am.AgentMessage) {
	u.sendQueue <- &agentMessage
}

// Opens a connection to Bastion
//
// In order for Connect to serve as a robust abstraction for other processes that rely on it,
// it must handle its own retry logic. For this, we use an exponential backoff. Some failures
// within the connection process are considered transient, and thus trigger a retry. Others are
// considered fatal, and return an error
func (u *UniversalConnection) connect(connUrl string, headers http.Header, params url.Values) error {
	// Determine our target endpoint and target select handler
	var endpoint string
	var targetSelectHandler func(msg am.AgentMessage) (string, error)
	switch u.channelType {
	case DaemonDataChannel:
		endpoint = daemonHubEndpoint
		targetSelectHandler = daemonTargetSelector
	case AgentDataChannel:
		endpoint = agentHubEndpoint
		targetSelectHandler = agentDataChannelTargetSelector
	case AgentControlChannel:
		endpoint = controlHubEndpoint
		targetSelectHandler = agentControlChannelTargetSelector
	}

	// Check if the connection url is a validly formatted url
	connectionUrl, err := url.Parse(connUrl)
	if err != nil {
		return err
	}
	connectionUrl.Path = path.Join(connectionUrl.Path, endpoint)

	// Make a context and tie it in with our tomb and then send it everywhere
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-u.tmb.Dying():
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
		case <-u.tmb.Dying():
			return nil
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			if u.channelType == AgentControlChannel {
				// First get the config from the vault
				config, err := vault.LoadVault()
				if err != nil {
					return fmt.Errorf("failed to retrieve agent vault: %s", err)
				}

				if solvedChallenge, err := challenge.Get(u.logger, connUrl, params["target_id"][0], params["version"][0], config.Data.PrivateKey, ctx); err != nil {
					u.logger.Debugf("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
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
			}

			if err := u.client.Connect(ctx, connectionUrl.String(), headers, params, targetSelectHandler); err != nil {
				u.logger.Debugf("Retrying in %s because of and error on connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
			} else {
				u.logger.Info("Connection successful!")

				if u.channelType != DaemonDataChannel {
					u.sendQueueReady = true
				}

				return nil
			}

			u.logger.Infof("Failed to connect to %s retrying in %s", connectionUrl.String(), backoffParams.NextBackOff().Round(time.Second))
		}
	}
}

func (u *UniversalConnection) waitForAgentReady() {
	select {
	case <-u.agentReadyChan:
		u.sendQueueReady = true
	case <-time.After(agentConnectedTimeout):
		u.Close(fmt.Errorf("timed out waiting for agent to connect"))
	}
}
