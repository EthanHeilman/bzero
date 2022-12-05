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
	rootCertHash               = "v3R6c5gJMEUCGrh743C7GfV9TjaGi1odcz0anP03zbA="
)

type Certificate struct {
	logger     *logger.Logger
	CACert     []byte
	ClientCert []byte
	ClientKey  []byte
}

type ClientCertificateRequest struct {
	TargetUser          string
	ClientCertificate   string
	RootCertificateHash string
}

type ClientCertificateResponse struct {
	CACertificate     []byte
	ClientCertificate []byte
	ClientKey         []byte
}

func (c *Certificate) TLSKeyPair() (tls.Certificate, error) {
	// c.logger.Infof("%s", c.ClientCert)
	// c.logger.Infof("%s", c.ClientKey)
	return tls.X509KeyPair(c.ClientCert, c.ClientKey)
}

func New(logger *logger.Logger, targetUser string) (*Certificate, error) {
	req := ClientCertificateRequest{
		TargetUser:          targetUser,
		ClientCertificate:   "",
		RootCertificateHash: rootCertHash,
	}

	logger.Infof("target user: %s", targetUser)

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

	logger.Infof("Client Certificate: %s", string(certResponse.ClientCertificate))
	logger.Infof("Client key: %s", string(certResponse.ClientKey))

	return &Certificate{
		logger:     logger,
		CACert:     certResponse.CACertificate,
		ClientCert: certResponse.ClientCertificate,
		ClientKey:  certResponse.ClientKey,
	}, nil
}
