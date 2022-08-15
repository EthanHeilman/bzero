/*
The Websocket package establishes and ferries raw bytes across the underlying websocket
connection. In terms of the overall connection layer architecture, this package is
responsible for providing the raw bytes to the protocol handler for it to parse and
handle.

More documentation about architecture to come in next part of the refactor.
*/

package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"bastionzero.com/bctl/v1/bzerolib/logger"
	gorilla "github.com/gorilla/websocket"
)

const (
	httpsOnlyWebsocketScheme = "wss"
	httpWebsocketScheme      = "ws"
)

var websocketUrlScheme = httpsOnlyWebsocketScheme

type Websocket struct {
	logger *logger.Logger
	client *gorilla.Conn

	// Variables for managing elegant shutdown
	doneChan chan struct{}
	isDead   bool
	err      error

	// Received messages
	inbound chan *[]byte
}

func New(logger *logger.Logger) *Websocket {
	return &Websocket{
		doneChan: make(chan struct{}),
		logger:   logger,
		inbound:  make(chan *[]byte, 200),
	}
}

func (w *Websocket) Close() {
	w.client.Close()
	w.isDead = true
}

func (w *Websocket) Done() <-chan struct{} {
	return w.doneChan
}

func (w *Websocket) Err() error {
	return w.err
}

func (w *Websocket) Inbound() <-chan *[]byte {
	return w.inbound
}

func (w *Websocket) Send(message []byte) error {
	return w.client.WriteMessage(gorilla.TextMessage, message)
}

func (w *Websocket) Dial(connUrl *url.URL, headers http.Header, ctx context.Context) (err error) {
	// Check to see if we have to reinitialize our variables in case this is post death
	if w.isDead {
		w.isDead = false
		w.doneChan = make(chan struct{})
	}

	// Make sure url scheme is correct
	connUrl.Scheme = websocketUrlScheme

	// Try to connect websocket once
	if w.client, _, err = gorilla.DefaultDialer.DialContext(ctx, connUrl.String(), headers); err != nil {
		return fmt.Errorf("error dialing websocket: %w", err)
	}

	go w.receive()

	return nil
}

func (w *Websocket) receive() {
	defer func() {
		w.isDead = true
		close(w.doneChan)
	}()

	for {
		// Read incoming message
		if _, rawMessage, err := w.client.ReadMessage(); w.isDead {
			return
		} else if err != nil {
			// Check if it's a clean exit
			if !gorilla.IsCloseError(err, gorilla.CloseNormalClosure) {
				w.logger.Error(err)
				w.err = err
			} else {
				w.logger.Info("Websocket connection closed normally")
			}
			return
		} else {
			w.inbound <- &rawMessage
		}
	}
}
