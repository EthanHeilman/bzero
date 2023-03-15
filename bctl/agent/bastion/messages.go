package bastion

import (
	"crypto/rsa"

	"github.com/bastionzero/go-toolkit/certificate/splitclient"
)

type ErrorReport struct {
	Reporter  string      `json:"reporter"`
	Timestamp string      `json:"timestamp"`
	Message   string      `json:"message"`
	State     interface{} `json:"state"`
}

type RestartReport struct {
	TargetId       string      `json:"targetId"`
	AgentPublicKey string      `json:"agentPublicKey"`
	Timestamp      string      `json:"timestamp"`
	Message        string      `json:"message"`
	State          interface{} `json:"state"`
}

type ClientCertificateRequest struct {
	TargetUser        string
	ClientCertificate splitclient.SplitClientCertificate
	ClientPublicKey   rsa.PublicKey
	KeyShardHash      string
}

type ClientCertificateResponse struct {
	ClientCertificate splitclient.SplitClientCertificate
}
