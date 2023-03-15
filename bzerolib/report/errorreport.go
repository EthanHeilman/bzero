package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"bastionzero.com/bzerolib/connection/httpclient"
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

func ReportError(ctx context.Context, serviceUrl string, errReport ErrorReport) error {
	errReport.State = fmt.Sprintf("%+v", errReport.State)

	// Marshal the request
	errBytes, err := json.Marshal(errReport)
	if err != nil {
		return fmt.Errorf("error marshalling error report: %+v", errReport)
	}
	body := bytes.NewBuffer(errBytes)

	client, err := httpclient.New(serviceUrl, httpclient.HTTPOptions{
		Endpoint: errorEndpoint,
		Body:     body,
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create our http client: %s", err)
	}

	if _, err := client.Post(ctx); err != nil {
		return fmt.Errorf("failed to report error: %s, Request: %+v", err, errReport)
	}

	return nil
}
