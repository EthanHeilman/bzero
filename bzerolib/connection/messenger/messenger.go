/*
The Messenger is the middle layer of our connection architecture, it is
responsible for speaking whatever protocol is going over the wire.

1. Transporter
2. Messenger
3. ConnectionManager

See note in connection.go for more information
*/
package messenger

import (
	"context"
	"net/http"
	"net/url"

	"bastionzero.com/bzerolib/connection/agentmessage"
	"bastionzero.com/bzerolib/connection/messenger/signalr"
)

type Messenger interface {
	Close(reason error)
	Done() <-chan struct{}
	Err() error
	Inbound() <-chan *signalr.SignalRMessage
	Connect(ctx context.Context, targetUrl string, headers http.Header, params url.Values, targetSelectHandler func(msg agentmessage.AgentMessage) (string, error)) error
	Send(message agentmessage.AgentMessage) error
}
