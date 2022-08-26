package messenger

import (
	"context"
	"net/http"
	"net/url"

	"bastionzero.com/bctl/v1/bzerolib/connection/agentmessage"
	"bastionzero.com/bctl/v1/bzerolib/connection/messenger/signalr"
)

type Messenger interface {
	Close(reason error)
	Done() <-chan struct{}
	Inbound() <-chan *signalr.SignalRMessage
	Connect(ctx context.Context, targetUrl string, headers http.Header, params url.Values, targetSelectHandler func(msg agentmessage.AgentMessage) (string, error)) error
	Send(message agentmessage.AgentMessage) error
}
