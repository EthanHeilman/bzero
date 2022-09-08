/*
The signalr package is a protocol handler. Since we currently use .NET in the backend,
we need to establish a connection via the SignalR protocol and to send and parse the
messages that are ferried over the underlying connection. It's this package's
responsibility to isolate and handle the SignalR logic.

More documentation about architecture to come in next part of the refactor.
*/
package signalr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	am "bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/transporter"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"gopkg.in/tomb.v2"
)

type SignalR struct {
	tmb      tomb.Tomb
	logger   *logger.Logger
	doneChan chan struct{}

	client  transporter.Transporter
	inbound chan *SignalRMessage

	// Function for choosing target method
	targetSelectHandler func(am.AgentMessage) (string, error)

	// Thread-safe implementation for tracking whether SignalR messages
	// are received/processed successfully or not
	invocator Invocator
}

func New(
	logger *logger.Logger,
	client transporter.Transporter,
) *SignalR {
	return &SignalR{
		logger:    logger,
		client:    client,
		doneChan:  make(chan struct{}),
		inbound:   make(chan *SignalRMessage, 200),
		invocator: NewInvocationTracker(),
	}
}

func (s *SignalR) Close(reason error) {
	if !s.tmb.Alive() {
		return
	}

	s.tmb.Kill(reason)
	s.tmb.Wait()
}

func (s *SignalR) Err() error {
	return s.tmb.Err()
}

func (s *SignalR) Done() <-chan struct{} {
	return s.doneChan
}

func (s *SignalR) Inbound() <-chan *SignalRMessage {
	return s.inbound
}

func (s *SignalR) Connect(
	ctx context.Context,
	targetUrl string,
	headers http.Header,
	params url.Values,
	targetSelectHandler func(msg am.AgentMessage) (string, error),
) error {
	s.targetSelectHandler = targetSelectHandler

	// Reset variables
	if !s.tmb.Alive() {
		s.tmb = tomb.Tomb{}
		s.doneChan = make(chan struct{})
	}

	// Normally SignalR would require a negotiate call here to initiate the connection,
	// however since we're only making websockets, we can omit that
	// https://github.com/aspnet/SignalR/blob/master/specs/TransportProtocols.md#websockets-full-duplex

	// Build our Url
	u, err := buildUrl(targetUrl, params)
	if err != nil {
		return err
	}

	// Connect to our endpoint
	s.logger.Infof("Making websocket connection")
	if err := s.client.Dial(u, headers, ctx); err != nil {
		return fmt.Errorf("failed to connect to endpoint %s: %w", u.String(), err)
	}

	// Negotiate our SignalR version
	// Ref: https://stackoverflow.com/questions/65214787/signalr-websockets-and-go
	s.logger.Infof("Initiating SignalR handshake")
	versionMessageBytes := append([]byte(`{"protocol": "json","version": 1}`), signalRMessageTerminatorByte)
	if err := s.client.Send(versionMessageBytes); err != nil {
		rerr := fmt.Errorf("failed to negotiate SignalR version: %w", err)
		s.client.Close(rerr)
		return rerr
	}

	s.logger.Infof("Sucessfully established SignalR protocol")

	// If the handshake was successful, then we've made our connection and we can
	// start listening and sending on it
	s.tmb.Go(func() error {
		defer s.logger.Info("SignalR processing done")
		defer close(s.doneChan)

		// Unwrap and forward inbound messages
		for {
			select {
			case <-s.tmb.Dying(): // death from Close() call
				err := s.avengersEndgame()
				if err != nil {
					s.logger.Error(err)
				}

				s.client.Close(err)
				return err
			case <-s.client.Done():
				return fmt.Errorf("closed websocket")
			case messageBytes := <-s.client.Inbound():
				if err := s.unwrap(*messageBytes); err != nil {
					s.logger.Errorf("error unwrapping SignalR message: %s", err)
				}
			}
		}
	})
	return nil
}

func (s *SignalR) avengersEndgame() error {
	finalCountdown := time.NewTicker(time.Minute)
	checkDoneInterval := time.Second

	for {
		select {
		case messageBytes := <-s.client.Inbound():
			if err := s.unwrap(*messageBytes); err != nil {
				s.logger.Errorf("error unwrapping SignalR message: %s", err)
			}
		case <-time.After(checkDoneInterval):
			if s.invocator.IsEmpty() {
				return nil
			}
		case <-finalCountdown.C:
			return fmt.Errorf("connection failed to close, forcing shutdown")
		}
	}
}

func (s *SignalR) unwrap(raw []byte) error {
	// We may have received multiple messages in one
	splitMessages := bytes.Split(raw, []byte{signalRMessageTerminatorByte})

	for _, rawMessage := range splitMessages {
		// Ignore empty slices AND empty json "{}"
		if len(rawMessage) <= 2 {
			continue
		}

		// Only grab the message type so we can switch on it
		var signalRMessageType MessageTypeOnly
		if err := json.Unmarshal(rawMessage, &signalRMessageType); err != nil {
			return fmt.Errorf("error unmarshalling SignalR message: %s", string(rawMessage))
		}

		switch SignalRMessageType(signalRMessageType.Type) {

		case Ping: // Ignore pings and don't log them because they happen so frequently

		// This SignalR close message can be thrown on `OnConnectedAsync` as a result of
		//  of failed param validation
		case Close:
			s.logger.Infof("received SignalR message to close the connection")
			s.Close(fmt.Errorf("close connection"))

		// These messages let us know if a previous message was received correctly
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

			// Push message to queue for processing
			s.inbound <- &message

		default:
			s.logger.Infof("Ignoring %s message", SignalRMessageType(signalRMessageType.Type).String())
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
		return fmt.Errorf("server error on message with method %s: %s", message.Target, *completionMessage.Error)
	} else if completionMessage.Result != nil && completionMessage.Result.Error {
		return fmt.Errorf("server error on message with method %s: %s", message.Target, *completionMessage.Result.ErrorMessage)
	}

	return nil
}

func (s *SignalR) Send(message am.AgentMessage) error {
	// Select SignalR Endpoint
	target, err := s.targetSelectHandler(message)
	if err != nil {
		return fmt.Errorf("error in selecting SignalR Endpoint target name: %w", err)
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal agent message: %w", err)
	}

	wrappedMessage := SignalRMessage{
		Target:    target,
		Type:      int(Invocation),
		Arguments: []json.RawMessage{messageBytes},
	}

	// Track our message so that we can correctly process the corresponding completion
	// message based on the "InvocationId"
	trackedMessage := s.invocator.Track(wrappedMessage)
	trackedMessageBytes, err := json.Marshal(trackedMessage)
	if err != nil {
		return fmt.Errorf("error marshalling outgoing SignalR Message: %+v", trackedMessage)
	}

	// SignalR messages require a special terminating character to let the server know
	// that it has received the entire message and can start processing it
	terminatedMessageBytes := append(trackedMessageBytes, signalRMessageTerminatorByte)

	// Write our message to our connection
	err = s.client.Send(terminatedMessageBytes)

	// Since above send could fail, we want to untrack our tracked message
	if err != nil {
		s.invocator.Match(trackedMessage.InvocationId)
	}

	return err
}

func buildUrl(serviceUrl string, params url.Values) (*url.URL, error) {
	// Build our websocket url object
	websocketUrl, err := url.ParseRequestURI(serviceUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection node service url %s: %w", serviceUrl, err)
	}

	// Set our params as encoded args
	websocketUrl.RawQuery = params.Encode()

	return websocketUrl, nil
}
