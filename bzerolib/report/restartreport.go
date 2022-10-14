package report

import (
	"encoding/json"

	"bastionzero.com/bctl/v1/bzerolib/bzhttp"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	restartEndpoint = "/api/v2/agent/restart"
)

type RestartReport struct {
	TargetId       string            `json:"targetId"`
	AgentPublicKey string            `json:"agentPublicKey"`
	Timestamp      string            `json:"timestamp"`
	Message        string            `json:"message"`
	State          map[string]string `json:"state"`
}

func ReportRestart(logger *logger.Logger, serviceUrl string, restartReport RestartReport) {
	endpoint, err := bzhttp.BuildEndpoint(serviceUrl, restartEndpoint)
	if err != nil {
		logger.Errorf("failed to build restart report %s", restartReport)
	}

	// Marshall the request
	restartBytes, err := json.Marshal(restartReport)
	if err != nil {
		logger.Errorf("error marshalling restart report: %+v", restartReport)
		return
	}

	if resp, err := bzhttp.Post(logger, endpoint, "application/json", restartBytes, map[string]string{}, map[string]string{}); err != nil {
		logger.Errorf("failed to report restart: %s, Endpoint: %s, Request: %+v, Response: %+v", err, endpoint, restartReport, resp)
	}
}
