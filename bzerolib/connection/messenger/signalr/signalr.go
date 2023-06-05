/*
The signalr package is a protocol handler. Since we currently use .NET in the backend,
we need to establish a connection via the SignalR protocol. It is responsible for
wrapping and unwrapping messages that are ferried over the underlying connection.
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

	am "bastionzero.com/bzerolib/connection/agentmessage"
	"bastionzero.com/bzerolib/connection/transporter"
	"bastionzero.com/bzerolib/logger"
	"gopkg.in/tomb.v2"
)

// Rate at which the signalR client sends pings to the server. The server is
// using the default client timeout of 30s so ensure we are sending pings at
// least 2x more frequently than that for some additional buffer. These
// messages only need to be sent if no other activity is occurring in the
// signalR websocket. Not a constant so it can be updated in unit tests
// Ref: https://github.com/dotnet/aspnetcore/blob/main/src/SignalR/docs/specs/HubProtocol.md#ping-aka-keep-alive
// variable only so it can be modified in unit tests
var ClientPingRate = 15 * time.Second

// Timeout before closing the connection after not receiving any messages from
// the server. The signalR server should be sending ping messages at least every
// 15s by default
// Ref: https://github.com/dotnet/aspnetcore/blob/34a16dcf5043b4f02756e9d0727943c385cd2810/src/SignalR/server/Core/src/HubOptions.cs#L26-L29
// variable only so it can be modified in unit tests
var ServerPingTimeout = 30 * time.Second

const (
	// Byte to indicate the end of a SignalR message
	TerminatorByte = 0x1E
)

type sendQueueMessage struct {
	msg      []byte
	doneChan chan error
}

type SignalR struct {
	tmb    tomb.Tomb
	logger *logger.Logger

	client  transporter.Transporter
	inbound chan *SignalRMessage

	// Function for choosing target method
	targetSelectHandler func(am.AgentMessage) (string, error)

	// Thread-safe implementation for tracking whether SignalR messages
	// are received/processed successfully or not
	invocator Invocator

	sendQueue chan sendQueueMessage
}

func New(
	logger *logger.Logger,
	client transporter.Transporter,
) *SignalR {
	return &SignalR{
		logger:    logger,
		client:    client,
		inbound:   make(chan *SignalRMessage, 200),
		invocator: NewInvocationTracker(),
		sendQueue: make(chan sendQueueMessage),
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
	return s.tmb.Dead()
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
		s.invocator = NewInvocationTracker()
	}

	// Normally SignalR would require a negotiate call here to initiate the connection,
	// however since we're only making websockets, we can omit that
	// https://github.com/dotnet/aspnetcore/blob/main/src/SignalR/docs/specs/TransportProtocols.md#websockets-full-duplex

	// Build our Url
	u, err := buildUrl(targetUrl, params)
	if err != nil {
		return err
	}

	// Connect to our endpoint
	s.logger.Infof("Making websocket connection")
	if err := s.client.Dial(u, headers, ctx); err != nil {
		return fmt.Errorf("error connecting to %s: %w", u.String(), err)
	}

	// Negotiate our SignalR version
	// Ref: https://stackoverflow.com/questions/65214787/signalr-websockets-and-go
	s.logger.Infof("Initiating SignalR handshake")
	versionMessageBytes := append([]byte(`{"protocol": "json","version": 1}`), TerminatorByte)
	if err := s.client.Send(versionMessageBytes); err != nil {
		rerr := fmt.Errorf("failed to negotiate SignalR version: %w", err)
		s.client.Close(rerr)
		return rerr
	}

	s.logger.Infof("Successfully established SignalR protocol")

	// If the handshake was successful, then we've made our connection and we can
	// start listening and sending on it
	s.tmb.Go(func() error {
		defer s.logger.Info("SignalR processing done")

		// Process send queue
		s.tmb.Go(func() error {
			// Send an initial ping message to the server. After receiving the
			// initial ping the server will timeout and close the connection
			// after 30s if no more messages are received
			if err := s.ping(); err != nil {
				return fmt.Errorf("Failed to send initial ping message to server. Error: %w", err)
			}

			ticker := time.NewTicker(ClientPingRate)
			defer ticker.Stop()

			for {
				select {
				case <-s.tmb.Dying():
					s.logger.Info("Send queue processing done because tmb is dying.")
					return nil
				case <-ticker.C:
					if err := s.ping(); err != nil {
						s.logger.Errorf("Failed to send ping message. Error: %s", err)
					}
				case sendQueueMessage, ok := <-s.sendQueue:
					if !ok {
						s.logger.Errorf("Send queue was closed")
						return nil
					}

					err := s.client.Send(sendQueueMessage.msg)
					sendQueueMessage.doneChan <- err
					ticker.Reset(ClientPingRate)
				}
			}
		})

		ticker := time.NewTicker(ServerPingTimeout)
		defer ticker.Stop()

		// Unwrap and forward inbound messages
		for {
			select {
			case <-s.tmb.Dying(): // death from Close() call
				err := s.avengersEndgame()
				if err != nil {
					s.logger.Error(err)
				}

				s.client.Close(s.Err())
				return err
			case <-s.client.Done():
				return fmt.Errorf("closed websocket")
			case <-ticker.C:
				err := fmt.Errorf("server ping timeout: failed to receive any messages from the server after %s", ServerPingTimeout)
				s.client.Close(err)
				return err
			case messageBytes := <-s.client.Inbound():
				ticker.Reset(ServerPingTimeout)
				if err := s.unwrap(*messageBytes); err != nil {
					s.logger.Errorf("error unwrapping SignalR message: %s", err)
				}
			}
		}
	})
	return nil
}

func (s *SignalR) ping() error {
	pingMessage := PingMessage{
		Type: int(Ping),
	}

	pingMessageBytes, err := json.Marshal(pingMessage)
	if err != nil {
		return fmt.Errorf("error marshalling outgoing SignalR Ping Message: %+v", pingMessageBytes)
	}

	return s.client.Send(append(pingMessageBytes, TerminatorByte))
}

func (s *SignalR) avengersEndgame() error {
	finalCountdown := time.NewTicker(time.Minute)
	checkDoneInterval := time.Second

	for {
		select {
		case <-s.client.Done():
			return nil
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
	splitMessages := bytes.Split(raw, []byte{TerminatorByte})

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
			if err := s.processCloseMessage(rawMessage); err != nil {
				return err
			}

		// These messages let us know if a previous message was received correctly
		// and provides us with the resulting error if not
		case Completion:
			if err := s.processCompletionMessage(rawMessage); err != nil {
				return err
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

func (s *SignalR) processCloseMessage(msg []byte) error {
	s.logger.Infof("received SignalR message to close the connection")

	var closeMessage CloseMessage
	if err := json.Unmarshal(msg, &closeMessage); err != nil {
		return fmt.Errorf("error unmarshalling SignalR close message: %s", string(msg))
	}

	s.tmb.Kill(&WebsocketNormalClosure{ServerError: closeMessage.Error})

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
	terminatedMessageBytes := append(trackedMessageBytes, TerminatorByte)

	sendQueueMessage := sendQueueMessage{
		msg:      terminatedMessageBytes,
		doneChan: make(chan error),
	}

	s.sendQueue <- sendQueueMessage
	err = <-sendQueueMessage.doneChan
	close(sendQueueMessage.doneChan)

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
