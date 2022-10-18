package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	errorEndpoint = "/api/v2/agent/error"
)

type ErrorReport struct {
	Reporter  string      `json:"reporter"`
	Timestamp string      `json:"timestamp"`
	Message   string      `json:"message"`
	State     interface{} `json:"state"`
	Logs      string      `json:"logs"`
}

func ReportError(logger *logger.Logger, ctx context.Context, serviceUrl string, errReport ErrorReport) {
	errReport.State = fmt.Sprintf("%+v", errReport.State)

	// Marshall the request
	errBytes, err := json.Marshal(errReport)
	if err != nil {
		logger.Errorf("error marshalling error report: %+v", errReport)
		return
	}
	body := bytes.NewBuffer(errBytes)

	client, err := httpclient.New(logger, serviceUrl, httpclient.HTTPOptions{
		Endpoint: errorEndpoint,
		Body:     body,
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
	})
	if err != nil {
		logger.Errorf("failed to create our http client: %s", err)
		return
	}

	if _, err := client.Post(ctx); err != nil {
		logger.Errorf("failed to report error: %s, Request: %+v", err, errReport)
	}
}
