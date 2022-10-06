/*
The Websocket package establishes and ferries raw bytes across the underlying websocket
connection. In terms of the overall connection layer architecture, this package is
at the lowest layer, providing the raw bytes to the protocol handler for it to parse and
handle.
*/

package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"bastionzero.com/bctl/v1/bzerolib/connection/transporter"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	gorilla "github.com/gorilla/websocket"
	"gopkg.in/tomb.v2"
)

const (
	HttpsOnlyWebsocketScheme = "wss"
	HttpWebsocketScheme      = "ws"
)

var WebsocketUrlScheme = HttpsOnlyWebsocketScheme

type Websocket struct {
	tmb    tomb.Tomb
	logger *logger.Logger
	client *gorilla.Conn

	// Received messages
	inbound chan *[]byte
}

func New(logger *logger.Logger) transporter.Transporter {
	return &Websocket{
		logger:  logger,
		inbound: make(chan *[]byte, 200),
	}
}

func (w *Websocket) Close(reason error) {
	if w.tmb.Alive() {
		w.logger.Infof("Websocket connection closing because: %s", reason)

		// close the websocket connection
		w.client.Close()

		w.tmb.Kill(reason)
		w.tmb.Wait()
	} else {
		w.logger.Infof("Close was called while in a dying state")
	}
}

func (w *Websocket) Done() <-chan struct{} {
	return w.tmb.Dead()
}

func (w *Websocket) Err() error {
	return w.tmb.Err()
}

func (w *Websocket) Inbound() <-chan *[]byte {
	return w.inbound
}

func (w *Websocket) Send(message []byte) error {
	if w.client != nil {
		return w.client.WriteMessage(gorilla.TextMessage, message)
	} else {
		return fmt.Errorf("cannot send message because websocket is closed")
	}
}

func (w *Websocket) Dial(connUrl *url.URL, headers http.Header, ctx context.Context) (err error) {
	// Make sure url scheme is correct
	connUrl.Scheme = WebsocketUrlScheme

	// Try to connect websocket once
	if w.client, _, err = gorilla.DefaultDialer.DialContext(ctx, connUrl.String(), headers); err != nil {
		return fmt.Errorf("error dialing websocket: %w", err)
	}

	// Reinitialize our variables in case this is post death
	w.tmb = tomb.Tomb{}

	w.tmb.Go(w.receive)

	return nil
}

func (w *Websocket) receive() error {
	defer w.logger.Infof("Websocket connection closed")
	w.logger.Infof("Websocket connection started")

	for {
		// Read incoming message
		if _, rawMessage, err := w.client.ReadMessage(); !w.tmb.Alive() {
			return nil
		} else if err != nil {
			// Check if it's a clean exit
			if !gorilla.IsCloseError(err, gorilla.CloseNormalClosure) {
				w.logger.Error(err)
			} else {
				w.logger.Info("Websocket connection closed normally")
			}
			return err
		} else {
			w.inbound <- &rawMessage
		}
	}
}
