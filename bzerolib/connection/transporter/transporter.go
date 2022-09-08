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
