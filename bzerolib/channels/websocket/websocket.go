package websocket

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
	am "bastionzero.com/bctl/v1/bzerolib/channels/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/channels/websocket/challenge"
	"bastionzero.com/bctl/v1/bzerolib/connection/broker"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
	newws "bastionzero.com/bctl/v1/bzerolib/connection/transporter/websocket"
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
	AgentConnectedWebsocketTimeout = 30 * time.Second
	MaximumReconnectWaitTime       = 1 * time.Hour
	closeTimeout                   = 10 * time.Second
)

type Messenger interface {
	Close(reason error)
	Done() <-chan struct{}
	Inbound() <-chan *signalr.SignalRMessage
	Connect(ctx context.Context, targetUrl string, headers http.Header, params url.Values, targetSelectHandler func(msg am.AgentMessage) (string, error)) error
	Send(message am.AgentMessage) error
}

type IWebsocket interface {
	Subscribe(id string, channel broker.IChannel)
	Close(reason error)
	Send(agentMessage am.AgentMessage)
	Done() <-chan struct{}
	Err() error
	Ready() bool
}

type Websocket struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	// This is our underlying connection where we send and receive messages
	client Messenger

	// A connection broker, allows us to narrowcast to one subscribed datachannel
	broker *broker.Broker

	// Buffered channel to keep track of outgoing messages
	sendQueue chan *am.AgentMessage

	// True when websocket is ready to start sending output messages
	sendQueueReady bool

	// Type of connection being made over the websocket
	channelType ChannelType

	// Agent Ready Channel indicates when the agent has connected to the
	// corresponding websocket. This is only used for daemon websocket.
	agentReadyChan chan struct{}
}

func New(
	logger *logger.Logger,
	connectionUrl string,
	params url.Values,
	headers http.Header,
	autoReconnect bool,
	wtype ChannelType,
) (*Websocket, error) {
	// Create our signalr object
	srLogger := logger.GetComponentLogger("SignalR")
	wsLogger := srLogger.GetComponentLogger("Websocket")
	conn := signalr.New(srLogger, newws.New(wsLogger))

	ws := Websocket{
		logger:         logger,
		client:         conn,
		broker:         broker.New(),
		sendQueue:      make(chan *am.AgentMessage, 50),
		channelType:    wtype,
		agentReadyChan: make(chan struct{}, 1),
	}

	if err := ws.connect(connectionUrl, headers, params); err != nil {
		logger.Error(err)
		ws.Close(fmt.Errorf("unable to connect to BastionZero"))
	}

	// Receive any messages in the websocket
	go ws.receive()

	ws.tmb.Go(func() error {
		ws.logger.Infof("Connection has started")
		defer ws.logger.Infof("Connection has stopped")

		for {
			select {
			case <-ws.tmb.Dying():
				ws.waitForRemainingMessages()
				ws.logger.Infof("Closing connection handlers")
				ws.client.Close(fmt.Errorf("connection closing"))
				return nil
			case <-ws.client.Done():
				ws.sendQueueReady = false

				if autoReconnect {
					logger.Infof("Lost connection to BastionZero, reconnecting...")

					if err := ws.connect(connectionUrl, headers, params); err != nil {
						logger.Errorf("failed to connect to BastionZero: %s", err)
						return err
					}
				} else {
					logger.Infof("Connection with BastionZero closed and we're not retrying")
					return nil
				}
			case message := <-ws.sendQueue:
				if !ws.sendQueueReady && ws.channelType == DaemonDataChannel {
					// If this is a daemon websocket connection wait for the agent to
					// connect before sending any messages from the output queue
					ws.waitForAgentWebsocketReady()
				}

				if err := ws.client.Send(*message); err != nil {
					ws.logger.Errorf("failed to send message: %s", err)
				}
			}
		}
	})

	return &ws, nil
}

func (w *Websocket) receive() {
	for {
		select {
		case <-w.tmb.Dead():
			return
		case message := <-w.client.Inbound():
			if err := w.processInbound(*message); err != nil {
				w.logger.Error(err)
			}
		}
	}
}

func (w *Websocket) waitForRemainingMessages() {
	w.logger.Infof("Entering endgame for websocket")
	exitTimer := time.Second
	absoluteTimeout := time.NewTimer(10 * time.Second)
	defer absoluteTimeout.Stop()

	for {
		select {
		case message := <-w.sendQueue:
			if !w.sendQueueReady && w.channelType == DaemonDataChannel {
				// If this is a daemon websocket connection wait for the agent to
				// connect before sending any messages from the output queue
				w.waitForAgentWebsocketReady()
			}

			if err := w.client.Send(*message); err != nil {
				w.logger.Errorf("failed to send message: %s", err)
			}
		case <-time.After(exitTimer):
			return
		case <-absoluteTimeout.C:
			w.logger.Errorf("timed out waiting for connection to receive all messages after close")
			return
		}
	}
}

func (w *Websocket) Ready() bool {
	return w.sendQueueReady
}

func (w *Websocket) Done() <-chan struct{} {
	return w.tmb.Dead()
}

func (w *Websocket) Err() error {
	return w.tmb.Err()
}

// Close kills all datachannels subscribed to this websocket, kills the
// websocket tomb, disconnects the websocket connection, and waits for all
// goroutines tracked by the websocket tomb to finish running
//
// if the websocket is trying to reconnect, closing will not block. Otherwise,
// the websocket's tomb has only until `closeTimeout` to finish before Close() returns
func (w *Websocket) Close(reason error) {
	defer w.logger.Infof("websocket done")

	if !w.tmb.Alive() {
		w.logger.Infof("Close was called while in a dying state. Returning immediately")
		return
	}

	var wasDisconnected bool
	if !w.Ready() {
		wasDisconnected = true
	}

	w.close(reason)

	// close() will set us to not ready so we can't check that directly. However if we weren't ready when
	// Close() was called, don't bother waiting for the tomb
	if wasDisconnected {
		w.logger.Infof("Close was called while in a not-ready state. Returning immediately")
		return
	}

	select {
	case <-w.tmb.Dead():
		return
	case <-time.After(closeTimeout):
		return
	}
}

// close kills all datachannels subscribed to this websocket, kills the
// websocket tomb, and disconnects the websocket connection.
//
// Tracked goroutines that wish to close the websocket must call this function,
// instead of the publicly exposed one, to ensure there is no deadlock.
func (w *Websocket) close(reason error) {
	w.logger.Infof("connection closing because: %s", reason)

	// close all of our existing datachannels
	w.broker.Close(reason)
	// mark the tmb as dying so we ignore any errors that occur when closing the
	// websocket
	w.tmb.Kill(reason)

	// close the websocket connection. This will cause errors when reading from
	// websocket in receive
	if w.client != nil {
		w.client.Close(reason)
	}
}

// add channel to channels dictionary for forwarding incoming messages
func (w *Websocket) Subscribe(id string, channel broker.IChannel) {
	w.broker.Subscribe(id, channel)
}

// Returns error on websocket closed
func (w *Websocket) processInbound(message signalr.SignalRMessage) error {
	switch message.Target {
	case agentDisconnected:
		rerr := errors.New("the bzero agent terminated the connection")
		w.Close(rerr)
		return rerr
	case agentConnected:
		// Signal the agentReady channel when we receive a message
		// from the connection node that the agent websocket is
		// connected
		var agentConnectedMessage AgentConnectedMessage
		if err := json.Unmarshal(message.Arguments[0], &agentConnectedMessage); err != nil {
			return fmt.Errorf("error unmarshalling agent connected message. Error: %s", err)
		}

		w.logger.Infof("Agent is connected and ready to receive for connection: %s", agentConnectedMessage.ConnectionId)

		w.agentReadyChan <- struct{}{}
	default:
		// Otherwise assume that the invocation contains a single AgentMessage argument
		if len(message.Arguments) != 1 {
			return fmt.Errorf("expected a single agent message argument but got %d arguments", len(message.Arguments))
		}

		var agentMessage am.AgentMessage
		if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
			return fmt.Errorf("error unmarshalling agent message from websocket method %s: %w", message.Target, err)
		}

		if err := w.broker.DirectMessage(agentMessage.ChannelId, agentMessage); err != nil {
			return fmt.Errorf("failed to forward agent message to datachannel: %w", err)
		}
	}
	return nil
}

// The go routine listening to this channel is only created on connection and is torn down
// when that connection is closed. Therefore, if someone wants to send a message this
// will block
func (w *Websocket) Send(agentMessage am.AgentMessage) {
	w.sendQueue <- &agentMessage
}

// Opens a connection to Bastion
//
// In order for Connect to serve as a robust abstraction for other processes that rely on it,
// it must handle its own retry logic. For this, we use an exponential backoff. Some failures
// within the connection process are considered transient, and thus trigger a retry. Others are
// considered fatal, and return an error
func (w *Websocket) connect(connUrl string, headers http.Header, params url.Values) error {
	// Determine our target endpoint and target select handler
	var endpoint string
	var targetSelectHandler func(msg am.AgentMessage) (string, error)
	switch w.channelType {
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
		case <-w.tmb.Dying():
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
		case <-w.tmb.Dying():
			return nil
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			if w.channelType == AgentControlChannel {
				// First get the config from the vault
				config, err := vault.LoadVault()
				if err != nil {
					return fmt.Errorf("failed to retrieve agent vault: %s", err)
				}

				if solvedChallenge, err := challenge.Get(w.logger, connUrl, params["target_id"][0], params["version"][0], config.Data.PrivateKey, ctx); err != nil {
					w.logger.Debugf("Retrying in %s because we failed to connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
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

			if err := w.client.Connect(ctx, connectionUrl.String(), headers, params, targetSelectHandler); err != nil {
				w.logger.Debugf("Retrying in %s because of and error on connect: %s", backoffParams.NextBackOff().Round(time.Second), err)
			} else {
				w.logger.Info("Connection successful!")

				if w.channelType != DaemonDataChannel {
					w.sendQueueReady = true
				}

				return nil
			}

			w.logger.Infof("Failed to connect to %s retrying in %s", connectionUrl.String(), backoffParams.NextBackOff().Round(time.Second))
		}
	}
}

func (w *Websocket) waitForAgentWebsocketReady() {
	select {
	case <-w.agentReadyChan:
		w.sendQueueReady = true
	case <-time.After(AgentConnectedWebsocketTimeout):
		w.Close(fmt.Errorf("timed out waiting for agent websocket to connect"))
	}
}
