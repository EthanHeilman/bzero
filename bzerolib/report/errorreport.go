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
	errorEndpoint = "/api/v2/agent/error"
)

type ErrorReport struct {
	Reporter  string            `json:"reporter"`
	Timestamp string            `json:"timestamp"`
	State     map[string]string `json:"state"`
	Message   string            `json:"message"`
	Logs      string            `json:"logs"`
}

func ReportError(logger *logger.Logger, ctx context.Context, serviceUrl string, errReport ErrorReport) {
	// Marshall the request
	errBytes, err := json.Marshal(errReport)
	if err != nil {
		logger.Errorf("error marshalling error report: %+v", errReport)
		return
	}
	body := bytes.NewBuffer(errBytes)

	client, err := httpclient.NewWithBackoff(logger, serviceUrl, httpclient.HTTPOptions{
		Endpoint: errorEndpoint,
		Body:     body,
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
	})
	if err != nil {
		logger.Errorf("failed to create our http client: %s", err)
	}

	if _, err := client.Post(ctx); err != nil {
		logger.Errorf("failed to report restart: %s, Request: %+v, Response Status: %s", err, errReport)
	}
}
