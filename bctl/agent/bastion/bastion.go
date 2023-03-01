package bastion

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"bastionzero.com/agent/bastion/agentidentity"
	"bastionzero.com/bzerolib/connection/httpclient"
	"bastionzero.com/bzerolib/logger"
	"github.com/bastionzero/go-toolkit/certificate/splitclient"
)

const jsonContentType string = "application/json"

type endpoint string

const (
	errorReportEndpoint       endpoint = "/api/v2/agent/error"
	restartReportEndpoint     endpoint = "/api/v2/agent/restart"
	logUploadEndpoint         endpoint = "/api/v2/upload-logs/agent"
	certificateCosignEndpoint endpoint = "/api/v2/certificate/cosign"
)

type ApiClient interface {
	ReportError(ctx context.Context, reporter string, reportErr error, state any) error
	ReportRestart(ctx context.Context, targetId, pubKey, reason string, state any) error
	ReportLogs(ctx context.Context, logs *bytes.Buffer, formDataContentType string) error
	CosignCertificate(targetUser string, clientCert *splitclient.SplitClientCertificate, clientPubKey rsa.PublicKey, keyShardHash string) (*splitclient.SplitClientCertificate, error)
}

// Client for sending authenticated http requests to the bastion
type Bastion struct {
	logger       *logger.Logger
	serviceUrl   string
	agentIdToken agentidentity.AgentIdentityToken
	agentVersion string
}

func New(logger *logger.Logger, serviceUrl string, agentIdProvider agentidentity.AgentIdentityToken, agentVersion string) *Bastion {
	return &Bastion{
		logger:       logger,
		serviceUrl:   serviceUrl,
		agentIdToken: agentIdProvider,
		agentVersion: agentVersion,
	}
}

func (b *Bastion) Url() string {
	return b.serviceUrl
}

func (b *Bastion) ReportError(ctx context.Context, reporter string, reportErr error, state any) error {
	report := ErrorReport{
		Reporter:  reporter,
		Timestamp: fmt.Sprint(time.Now().UTC().Unix()),
		Message:   reportErr.Error(),
		State:     fmt.Sprintf("%+v", state),
	}

	reportBytes, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("error marshalling request: %s", err)
	}

	// No reponse payload
	_, err = b.request(ctx, bytes.NewBuffer(reportBytes), errorReportEndpoint, jsonContentType)

	return err
}

func (b *Bastion) ReportRestart(ctx context.Context, targetId, pubKey, reason string, state any) error {
	report := RestartReport{
		TargetId:       targetId,
		AgentPublicKey: pubKey,
		Timestamp:      fmt.Sprint(time.Now().UTC().Unix()),
		Message:        reason,
		State:          fmt.Sprintf("%+v", state),
	}

	reportBytes, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("error marshalling request: %s", err)
	}

	// No response payload
	_, err = b.request(ctx, bytes.NewBuffer(reportBytes), restartReportEndpoint, jsonContentType)

	return err
}

func (b *Bastion) ReportLogs(ctx context.Context, logs *bytes.Buffer, formDataContentType string) error {
	// No response payload
	_, err := b.request(ctx, logs, logUploadEndpoint, formDataContentType)

	return err
}

func (b *Bastion) CosignCertificate(targetUser string, clientCert *splitclient.SplitClientCertificate, clientPubKey rsa.PublicKey, keyShardHash string) (*splitclient.SplitClientCertificate, error) {
	req := ClientCertificateRequest{
		TargetUser:        targetUser,
		ClientCertificate: *clientCert,
		ClientPublicKey:   clientPubKey,
		KeyShardHash:      keyShardHash,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error marshalling request: %s", err)
	}

	resp, err := b.request(context.TODO(), bytes.NewBuffer(reqBytes), certificateCosignEndpoint, jsonContentType)
	if err != nil {
		return nil, err
	}

	var certResponse ClientCertificateResponse
	if err := json.NewDecoder(resp.Body).Decode(&certResponse); err != nil {
		return nil, fmt.Errorf("malformed certificate cosign response body: %w", err)
	} else {
		return &certResponse.ClientCertificate, nil
	}
}

func (b *Bastion) request(ctx context.Context, body *bytes.Buffer, apiEndpoint endpoint, contentType string) (*http.Response, error) {
	if apiEndpoint == "" {
		return nil, fmt.Errorf("unrecognized bastion api endpoint")
	}

	agentIdentityToken, err := b.agentIdToken.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent identity token: %s", err)
	}

	client, err := httpclient.New(b.serviceUrl, httpclient.HTTPOptions{
		Endpoint: string(apiEndpoint),
		Body:     body,
		Headers: http.Header{
			"Content-Type":  {contentType},
			"Authorization": []string{fmt.Sprintf("Bearer %s", agentIdentityToken)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate http client: %s", err)
	}

	return client.Post(ctx)
}
