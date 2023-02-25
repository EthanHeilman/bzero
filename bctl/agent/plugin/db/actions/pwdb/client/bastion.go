package client

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"github.com/bastionzero/go-toolkit/certificate/splitclient"
	"github.com/bastionzero/keysplitting"
	"golang.org/x/crypto/sha3"
)

const (
	certificateServiceEndpoint = "api/v2/certificate/cosign"
)

type ClientCertificateRequest struct {
	TargetUser        string
	ClientCertificate splitclient.SplitClientCertificate
	ClientPublicKey   rsa.PublicKey
	KeyShardHash      string
}

type ClientCertificateResponse struct {
	ClientCertificate splitclient.SplitClientCertificate
}

type BastionClient struct {
	serviceUrl            string
	agentIdentityProvider agentidentity.IAgentIdentityProvider
}

func New(serviceUrl string, agentIdProvider agentidentity.IAgentIdentityProvider) *BastionClient {
	return &BastionClient{
		serviceUrl:            serviceUrl,
		agentIdentityProvider: agentIdProvider,
	}
}

func (b *BastionClient) RequestCosign(targetUser string, clientCert *splitclient.SplitClientCertificate, clientPubKey rsa.PublicKey, privKey keysplitting.PrivateKeyShard) (*splitclient.SplitClientCertificate, error) {
	// Hash the agent's private key as an identifier for which certificate Bastion needs
	agentKeyPem, err := privKey.EncodePEM()
	if err != nil {
		return nil, fmt.Errorf("failed to encode split private key: %s", err)
	}

	hash := sha3.Sum256([]byte(agentKeyPem))
	agentKeyHash := base64.StdEncoding.EncodeToString(hash[:])

	req := ClientCertificateRequest{
		TargetUser:        targetUser,
		ClientCertificate: *clientCert,
		ClientPublicKey:   clientPubKey,
		KeyShardHash:      agentKeyHash,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error marshalling request to sign client certificate request: %s", err)
	}

	agentIdentityToken, err := b.agentIdentityProvider.GetToken(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to get agent identity token: %s", err)
	}

	client, err := httpclient.New(b.serviceUrl, httpclient.HTTPOptions{
		Endpoint: certificateServiceEndpoint,
		Body:     bytes.NewBuffer(reqBytes),
		Headers: http.Header{
			"Content-Type":  {"application/json"},
			"Authorization": []string{fmt.Sprintf("Bearer %s", agentIdentityToken)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error while instantiating http client: %s", err)
	}

	rsp, err := client.Post(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to get signed certificate: %s", err)
	}

	var certResponse ClientCertificateResponse
	if err := json.NewDecoder(rsp.Body).Decode(&certResponse); err != nil {
		return nil, fmt.Errorf("malformed certificate response: %s", err)
	}

	return &certResponse.ClientCertificate, nil
}
