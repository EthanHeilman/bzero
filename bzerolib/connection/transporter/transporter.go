/*
The lowest layer of the connection architecture used for any direct communication over
the wire, typically in bytes.

1. Transporter
2. Messenger
3. ConnectionManager

See note in connection.go for more information
*/
package transporter

import (
	"context"
	"net/http"
	"net/url"
)

type Transporter interface {
	Done() <-chan struct{}
	Err() error
	Inbound() <-chan *[]byte
	Dial(connUrl *url.URL, headers http.Header, ctx context.Context) (err error)
	Send(message []byte) error
	Close(reason error)
}
