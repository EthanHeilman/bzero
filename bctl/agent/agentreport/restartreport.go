package agentreport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"bastionzero.com/bzerolib/connection/httpclient"
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

func ReportRestart(ctx context.Context, serviceUrl string, restartReport RestartReport) error {
	restartReport.State = fmt.Sprintf("%+v", restartReport.State)

	// Marshall the request
	restartBytes, err := json.Marshal(restartReport)
	if err != nil {
		return fmt.Errorf("error marshalling restart report: %s", err)
	}
	body := bytes.NewBuffer(restartBytes)

	client, err := httpclient.New(serviceUrl, httpclient.HTTPOptions{
		Endpoint: restartEndpoint,
		Body:     body,
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create our http client: %s", err)
	}

	if _, err := client.Post(ctx); err != nil {
		return fmt.Errorf("failed to report restart: %s. Report: %+v", err, restartReport)
	}

	return nil
}
