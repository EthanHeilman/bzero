package controlserver

import (
	"fmt"
	"net/http"

	"bastionzero.com/bzerolib/logger"
)

type ControlServer struct {
	logger       *logger.Logger
	port         string
	shutdownChan chan struct{}
}

func New(logger *logger.Logger, port string, shutdownChan chan struct{}) *ControlServer {
	return &ControlServer{logger: logger, port: port, shutdownChan: shutdownChan}
}

func (c *ControlServer) ReceivedShutdown() chan struct{} {
	return c.shutdownChan
}

func (c *ControlServer) Start() {
	go func() {
		c.logger.Debugf("Starting control server on localhost:%s", c.port)

		http.HandleFunc("/shutdown", c.shutdown)

		if err := http.ListenAndServe(fmt.Sprintf(":%s", c.port), nil); err != nil {
			c.logger.Error(err)
		}
	}()
}

func (c *ControlServer) shutdown(w http.ResponseWriter, req *http.Request) {
	c.logger.Infof("Received shutdown request")
	c.shutdownChan <- struct{}{}
}
