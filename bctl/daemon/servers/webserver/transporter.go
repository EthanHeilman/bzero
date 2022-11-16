package webserver

import (
	"net/http"

	"bastionzero.com/bctl/v1/bzerolib/logger"
)

type Writer struct {
}

type Transport struct {
	logger *logger.Logger
}

func NewTransport(logger *logger.Logger) *Transport {
	return &Transport{
		logger: logger,
	}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.logger.Infof("GOT IT: %+v", req)

	return &http.Response{StatusCode: http.StatusOK}, nil
}
