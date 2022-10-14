package report

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	restartEndpoint = "/api/v2/agent/restart"
)

type RestartReport struct {
	TargetId       string      `json:"targetId"`
	AgentPublicKey string      `json:"agentPublicKey"`
	Timestamp      string      `json:"timestamp"`
	Message        string      `json:"message"`
	State          interface{} `json:"state"`
}

func ReportRestart(logger *logger.Logger, ctx context.Context, serviceUrl string, restartReport RestartReport) {
	// Marshall the request
	restartBytes, err := json.Marshal(restartReport)
	if err != nil {
		logger.Errorf("error marshalling restart report: %+v", restartReport)
		return
	}
	body := bytes.NewBuffer(restartBytes)

	client, err := httpclient.NewWithBackoff(logger, serviceUrl, httpclient.HTTPOptions{
		Endpoint: restartEndpoint,
		Body:     body,
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
	})
	if err != nil {
		logger.Errorf("failed to create our http client: %s", err)
	}

	if _, err := client.Post(ctx); err != nil {
		logger.Errorf("failed to report restart: %s, Request: %+v", err, restartReport)
	}
}
