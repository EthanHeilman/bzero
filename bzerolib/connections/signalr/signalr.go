package signalr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bastionzero.com/bctl/v1/bzerolib/bzhttp"
	am "bastionzero.com/bctl/v1/bzerolib/channels/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connections/broadcast"
	"bastionzero.com/bctl/v1/bzerolib/connections/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/connections/signalr/invocation"
	"bastionzero.com/bctl/v1/bzerolib/connections/websocket"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"github.com/cenkalti/backoff"
	"gopkg.in/tomb.v2"
)

type ISignalR interface {
}

type SignalR struct {
	tmb      tomb.Tomb
	logger   logger.Logger
	doneChan chan struct{}

	client   *websocket.Websocket
	outbound chan *am.AgentMessage

	endpoint string
	params   map[string][]string

	// Function for choosing target method
	targetSelector func(am.AgentMessage) (string, error)

	// Used for broadcasting the same recieved agent message to any number of
	// listeners
	broadcaster *broadcast.Broadcast

	// Thread-safe implementation for tracking invocation messages
	invocator *invocation.Invocation
}

func New(
	logger logger.Logger,
	endpoint string,
	params map[string][]string,
	targetSelector func(am.AgentMessage) (string, error),
) *SignalR {
	// Add the client protocol for SignalR
	// LUCIE: figure out if I actually need this header; took it from bzhttp
	params["clientProtocol"] = []string{"1.5"}

	return &SignalR{
		logger:         logger,
		doneChan:       make(chan struct{}),
		outbound:       make(chan *am.AgentMessage, 200),
		endpoint:       endpoint,
		params:         params,
		targetSelector: targetSelector,
		broadcaster:    broadcast.New(),
		invocator:      invocation.New(),
	}
}

func (s *SignalR) Close() {
	if s.tmb.Alive() {
		s.client.Close()

		s.tmb.Kill(nil)
		s.tmb.Wait()
	}
}

func (s *SignalR) Done() <-chan struct{} {
	return s.doneChan
}

func (s *SignalR) Subscribe(id string, channel broadcast.IChannel) {
	s.broadcaster.Subscribe(id, channel)
}

func (s *SignalR) Unsubscribe(id string) {
	s.broadcaster.Unsubscribe(id)
}

func (s *SignalR) Send(msg *am.AgentMessage) {
	s.outbound <- msg
}

func (s *SignalR) Connect() error {
	backoffParams := backoff.NewExponentialBackOff()

	// Configure our exponential backoff
	backoffParams.MaxElapsedTime = time.Hour * 72 // Wait in total at most 72 hours
	backoffParams.MaxInterval = time.Minute * 15  // At most 15 minutes in between requests

	ticker := backoff.NewTicker(backoffParams)
	for {
		select {
		case _, ok := <-ticker.C:
			if !ok {
				return fmt.Errorf("failed to connect after %s", backoffParams.MaxElapsedTime)
			}

			if err := s.handshake(); err != nil {
				s.logger.Errorf("retrying in %d because of error on connect: %w", backoffParams.NextBackOff().Round(time.Second), err)
			} else {
				s.logger.Infof("Connection successful to %s", s.endpoint)
				return nil
			}
		}
	}

	// Setup our processes for reading and writing from and to the connection
	s.tmb.Go(func() error {
		defer close(s.doneChan)
		defer s.client.Close()

		s.tmb.Go(func() error {
			for {
				select {
				case <-s.tmb.Dying():
					return nil
				case msg := <-s.outbound:
					if err := s.wrap(*msg); err != nil {
						s.logger.Errorf("failed to send agent message: %w", err)
					}
				}
			}
		})

		// Unwrap and forward incoming messages
		for {
			select {
			case <-s.tmb.Dying():
				return nil
			case <-s.client.Done():
				return fmt.Errorf("connection died")
			case rawMsg := <-s.client.Inbound():
				if err := s.unwrap(*rawMsg); err != nil {
					s.logger.Errorf("error processing raw message from websocket: %w", err)
				}
			}
		}
	})

	return nil
}

func (s *SignalR) handshake() error {
	// Make negotiation call to initiate handshake
	if err := s.negotiate(); err != nil {
		return fmt.Errorf("failed to complete SignalR handshake: %w", err)
	}

	// Connect to our endpoint
	if err := s.client.Dial(s.endpoint, s.params); err != nil {
		return fmt.Errorf("failed to connect to endpoint %s: %w", s.endpoint, err)
	}

	// Negotiate our SignalR version
	// Ref: https://stackoverflow.com/questions/65214787/signalr-websockets-and-go
	versionMessageBytes := append([]byte(`{"protocol": "json","version": 1}`), signalRMessageTerminatorByte)
	if err := s.client.Send(versionMessageBytes); err != nil {
		return fmt.Errorf("failed to negotiate SignalR version: %w", err)
	}

	return nil
}

func (s *SignalR) negotiate() error {
	negotiateEndpoint, err := bzhttp.BuildEndpoint(s.endpoint, "negotiate")
	if err != nil {
		return err
	}

	client := httpclient.New(s.logger, negotiateEndpoint, []byte{}, make(map[string][]string), s.params)

	// Make negotiate call
	// LUCIE: make it a real context
	_, err = client.Post(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to make negotiate POST: %w", err)
	}

	return nil
}

func (s *SignalR) unwrap(raw []byte) error {
	// We may have received multiple messages in one
	splitMessages := bytes.Split(raw, []byte{signalRMessageTerminatorByte})

	for _, rawMessage := range splitMessages {
		if len(rawMessage) == 0 {
			continue
		}

		// Only grab the message type so we can switch on it
		var signalRMessageType MessageTypeOnly
		if err := json.Unmarshal(rawMessage, &signalRMessageType); err != nil {
			return fmt.Errorf("error unmarshalling SignalR message: %s", string(rawMessage))
		}

		switch SignalRMessageType(signalRMessageType.Type) {

		// These messages let us know if a previous message was recieved correctly
		// and provides us with the resulting error if not
		case Completion:
			if err := s.processCompletionMessage(rawMessage); err != nil {
				s.logger.Error(err)
			}

		// These messages are regular SignalR messages that we'll process and
		// forward to whoever is listening
		case Invocation:
			var message SignalRMessage
			if err := json.Unmarshal(rawMessage, &message); err != nil {
				return fmt.Errorf("error unmarshalling SignalR message: %s. Error: %w", string(rawMessage), err)
			}

			// Enforce assumption that there is only one AgentMessage in each SignalR wrapper
			if len(message.Arguments) != 1 {
				return fmt.Errorf("expected a single agent message but got %d arguments", len(message.Arguments))
			}

			// Extract out the AgentMessage
			var agentMessage am.AgentMessage
			if err := json.Unmarshal(message.Arguments[0], &agentMessage); err != nil {
				return fmt.Errorf("error unmarshalling agent message from websocket with method %s. Error: %w", message.Target, err)
			}

			// Push message to whoever's listening
			s.broadcaster.Broadcast(agentMessage)

		default:
			s.logger.Infof("Ignoring SignalR message with type %v", SignalRMessageType(signalRMessageType.Type))
		}
	}

	return nil
}

func (s *SignalR) processCompletionMessage(msg []byte) error {
	var completionMessage CompletionMessage
	if err := json.Unmarshal(msg, &completionMessage); err != nil {
		return fmt.Errorf("error unmarshalling SignalR completion message: %s", string(msg))
	}

	// A completion message is only valuable as long as it's referring to an existing, sent message
	if completionMessage.InvocationId == nil {
		return fmt.Errorf("received completion message without an invocationId: %s", string(msg))
	}

	invocationId := *completionMessage.InvocationId
	message, ok := s.invocator.Match(invocationId)
	if !ok {
		return fmt.Errorf("received completion message for a message we did not send")
	}

	// Check if our completion message is trying to let us know an error happened on the server while
	// processing the message
	if completionMessage.Error != nil {
		return fmt.Errorf("server error on message type %s: %s", message.MessageType, *completionMessage.Error)
	} else if completionMessage.Result != nil && completionMessage.Result.Error {
		return fmt.Errorf("server error on message type %s: %s", message.MessageType, *completionMessage.Result.ErrorMessage)
	}

	return nil
}

func (s *SignalR) wrap(message am.AgentMessage) error {
	// Select SignalR Endpoint
	target, err := s.targetSelector(message)
	if err != nil {
		return fmt.Errorf("error in selecting SignalR Endpoint target name: %w", err)
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("Failed to marshal agent message: %w", err)
	}

	invocationId := s.invocator.GetInvocationId()

	wrappedMessage := SignalRMessage{
		Target:       target,
		Type:         int(Invocation),
		Arguments:    []json.RawMessage{messageBytes},
		InvocationId: &invocationId,
	}

	msgBytes, _ := json.Marshal(wrappedMessage)
	if err != nil {
		return fmt.Errorf("error marshalling outgoing SignalR Message: %+v", wrappedMessage)
	}

	// Write our message to our connection
	err = s.client.Send(msgBytes)

	// Only track the message once we're absolutely sure it's been sent off
	// this protects our invocator from tracking multiple messages with the
	// same invocator ID
	if err != nil {
		s.invocator.Track(message)
	}

	return err
}
