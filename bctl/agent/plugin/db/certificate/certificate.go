package certificate

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"

	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	"bastionzero.com/bctl/v1/bzerolib/logger"
)

const (
	certificateServiceEndpoint = "https://lucie-certificate-service.bastionzero.com/generate/client"
	rootCertHash               = "vSfp3ArVJ14u7FNJcK6FCNeN726wg1Movv2+uC2l+4Q="
)

type Certificate struct {
	logger     *logger.Logger
	CACert     []byte
	ClientCert []byte
	ClientKey  string
}

type ClientCertificateRequest struct {
	targetUser          string
	ClientCertificate   string
	RootCertificateHash string
}

type ClientCertificateResponse struct {
	CACertificate     []byte
	ClientCertificate []byte
}

func (c *Certificate) TLSKeyPair() (tls.Certificate, error) {
	// c.logger.Infof("%s", c.ClientCert)
	// c.logger.Infof("%s", c.ClientKey)
	return tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientKey))
}

func New(logger *logger.Logger, targetUser string) (*Certificate, error) {
	req := ClientCertificateRequest{
		targetUser:          targetUser,
		ClientCertificate:   "",
		RootCertificateHash: rootCertHash,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("error marshalling request to sign client certificate request: %s", err)
	}

	client, err := httpclient.New(certificateServiceEndpoint, httpclient.HTTPOptions{
		Body: bytes.NewBuffer(reqBytes),
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

	return &Certificate{
		logger:     logger,
		CACert:     certResponse.CACertificate,
		ClientCert: certResponse.ClientCertificate,
		ClientKey: `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`,
	}, nil
}
